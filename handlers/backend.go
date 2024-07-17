package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"image/jpeg"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fiber-postgres-docker/config"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/minio/minio-go/v7"
	"github.com/unidoc/unipdf/v3/extractor"
	"github.com/unidoc/unipdf/v3/model"
	"github.com/unidoc/unipdf/v3/render"
)

var (
	db          *sqlx.DB      = config.Db
	minioClient *minio.Client = config.MinioClient
)

type PDF struct {
	ID         int       `db:"id" json:"id"`
	FileName   string    `db:"file_name"`
	UploadTime time.Time `db:"upload_time"`
	FileSize   int64     `db:"file_size"`
}

type User struct {
	ID       int    `db:"id" json:"id"`
	Username string `db:"username" json:"username"`
	Password string `db:"password"`
}

func CreateTable() {
	schema := `
    CREATE TABLE IF NOT EXISTS users (
        id SERIAL PRIMARY KEY,
        username VARCHAR(255) NOT NULL UNIQUE,
        password VARCHAR(255) NOT NULL
    );

    CREATE TABLE IF NOT EXISTS pdfs (
        id SERIAL PRIMARY KEY,
        file_name VARCHAR(255) NOT NULL,
        upload_time TIMESTAMP NOT NULL,
        file_size BIGINT NOT NULL
    );

    CREATE TABLE IF NOT EXISTS sentences (
        id SERIAL PRIMARY KEY,
        pdf_id INT REFERENCES pdfs(id),
        sentence TEXT NOT NULL
    );`
	_, err := db.Exec(schema)
	if err != nil {
		log.Fatalf("Failed to create tables: %v", err)
	}
}

func UploadPDF(c *fiber.Ctx) error {
	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "failed to upload file"})
	}

	fileName := file.Filename
	uploadTime := time.Now()

	tempFilePath := filepath.Join(os.TempDir(), fileName)
	err = c.SaveFile(file, tempFilePath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to save file"})
	}
	defer os.Remove(tempFilePath)

	fileInfo, err := os.Stat(tempFilePath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to get file info"})
	}
	fileSize := fileInfo.Size()

	_, err = minioClient.FPutObject(context.Background(), "pdfs", fileName, tempFilePath, minio.PutObjectOptions{ContentType: "application/pdf"})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to upload file to MinIO"})
	}

	var pdfID int
	err = db.QueryRowx(`INSERT INTO pdfs (file_name, upload_time, file_size) VALUES ($1, $2, $3) RETURNING id`,
		fileName, uploadTime, fileSize).Scan(&pdfID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to store PDF metadata"})
	}

	sentences, err := ParsePDF(tempFilePath)
	if err != nil {
		log.Printf("Failed to parse PDF: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to parse PDF"})
	}

	for _, sentence := range sentences {
		_, err = db.Exec(`INSERT INTO sentences (pdf_id, sentence) VALUES ($1, $2)`, pdfID, sentence)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to store sentences"})
		}
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "file uploaded successfully"})
}

func ParsePDF(filePath string) ([]string, error) {

	f, err := os.Open(filePath)
	if err != nil {
		log.Printf("Error opening file: %v", err)
		return nil, err
	}
	defer f.Close()

	pdfReader, err := model.NewPdfReader(f)
	if err != nil {
		log.Printf("Error creating PDF reader: %v", err)
		return nil, err
	}

	var sentences []string
	numPages, err := pdfReader.GetNumPages()
	if err != nil {
		log.Printf("Error getting number of pages: %v", err)
		return nil, err
	}

	for i := 1; i <= numPages; i++ {
		page, err := pdfReader.GetPage(i)
		if err != nil {
			log.Printf("Error getting page %d: %v", i, err)
			return nil, err
		}

		ex, err := extractor.New(page)
		if err != nil {
			log.Printf("Error creating extractor for page %d: %v", i, err)
			return nil, err
		}

		text, err := ex.ExtractText()
		if err != nil {
			log.Printf("Error extracting text from page %d: %v", i, err)
			return nil, err
		}

		pageSentences := strings.Split(text, ".")
		sentences = append(sentences, pageSentences...)
	}

	return sentences, nil
}

func ListPDFs(c *fiber.Ctx) error {
	var pdfs []PDF
	err := db.Select(&pdfs, `SELECT id, file_name, upload_time, file_size FROM pdfs`)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to list PDFs"})
	}

	return c.Status(fiber.StatusOK).JSON(pdfs)
}

func GetPDF(c *fiber.Ctx) error {
	id := c.Params("id")
	var pdf PDF
	err := db.Get(&pdf, `SELECT id, file_name, upload_time, file_size FROM pdfs WHERE id = $1`, id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "PDF not found"})
	}

	var sentences []string
	err = db.Select(&sentences, `SELECT sentence FROM sentences WHERE pdf_id = $1`, id)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to retrieve sentences"})
	}

	response := fiber.Map{
		"pdf":       pdf,
		"sentences": sentences,
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

func SearchWord(c *fiber.Ctx) error {
	keyword := c.Query("keyword")
	if keyword == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "keyword is required"})
	}

	var results []struct {
		PDFID    int    `db:"pdf_id"`
		Sentence string `db:"sentence"`
	}
	err := db.Select(&results, `SELECT pdf_id, sentence FROM sentences WHERE sentence ILIKE '%' || $1 || '%'`, keyword)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to search sentences"})
	}

	response := fiber.Map{
		"keyword":     keyword,
		"occurrences": len(results),
		"sentences":   results,
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

func GetPageAsImage(c *fiber.Ctx) error {
	id := c.Params("id")
	pageNumber, err := strconv.Atoi(c.Params("page"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid page number"})
	}

	var fileName string
	err = db.Get(&fileName, `SELECT file_name FROM pdfs WHERE id = $1`, id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "PDF not found"})
	}

	tempFilePath := filepath.Join(os.TempDir(), fileName)
	err = minioClient.FGetObject(context.Background(), "pdfs", fileName, tempFilePath, minio.GetObjectOptions{})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to download PDF from storage"})
	}
	defer os.Remove(tempFilePath)

	f, err := os.Open(tempFilePath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to open PDF file"})
	}
	defer f.Close()

	pdfReader, err := model.NewPdfReader(f)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create PDF reader"})
	}

	page, err := pdfReader.GetPage(pageNumber)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to get PDF page"})
	}

	r := render.NewImageDevice()
	img, err := r.Render(page)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to render PDF page to image"})
	}

	var imgBuf bytes.Buffer
	err = jpeg.Encode(&imgBuf, img, nil)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to encode image"})
	}

	c.Set("Content-Type", "image/jpeg")
	c.Send(imgBuf.Bytes())

	return nil
}

func DeletePDF(c *fiber.Ctx) error {
	id := c.Params("id")

	var fileName string
	err := db.Get(&fileName, `SELECT file_name FROM pdfs WHERE id = $1`, id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "PDF not found"})
	}

	err = minioClient.RemoveObject(context.Background(), "pdfs", fileName, minio.RemoveObjectOptions{})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to delete PDF from storage"})
	}

	_, err = db.Exec(`DELETE FROM sentences WHERE pdf_id = $1`, id)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to delete sentences"})
	}

	_, err = db.Exec(`DELETE FROM pdfs WHERE id = $1`, id)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to delete PDF metadata"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "PDF and related data deleted successfully"})
}

func AuthMiddleware() fiber.Handler {
	return basicauth.New(basicauth.Config{
		Authorizer: func(username, password string) bool {
			var user User
			err := db.Get(&user, "SELECT * FROM users WHERE username=$1", username)
			if err != nil {
				return false
			}
			return user.Password == password
		},
		Unauthorized: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
		},
	})
}

func Register(c *fiber.Ctx) error {
	var user User
	if err := c.BodyParser(&user); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "cannot parse JSON"})
	}

	var count int
	err := db.Get(&count, "SELECT COUNT(*) FROM users WHERE username=$1", user.Username)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "database error"})
	}
	if count > 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username already taken"})
	}
	_, err = db.Exec("INSERT INTO users (username, password) VALUES ($1, $2)", user.Username, user.Password)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "database error"})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"message": "user registered successfully"})
}

func Login(c *fiber.Ctx) error {
	username, password, err := BasicAuth(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid credentials"})
	}

	var user User
	err = db.Get(&user, "SELECT * FROM users WHERE username=$1", username)
	if err != nil {
		if err == sql.ErrNoRows {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid credentials"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "database error"})
	}

	if user.Password != password {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid credentials"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "login successful"})
}

func BasicAuth(c *fiber.Ctx) (string, string, error) {
	auth := c.Get("Authorization")
	if auth == "" {
		return "", "", fmt.Errorf("missing Authorization header")
	}

	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || parts[0] != "Basic" {
		return "", "", fmt.Errorf("invalid Authorization header")
	}

	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("invalid base64 encoding")
	}

	creds := strings.SplitN(string(decoded), ":", 2)
	if len(creds) != 2 {
		return "", "", fmt.Errorf("invalid Authorization header")
	}

	return creds[0], creds[1], nil
}
