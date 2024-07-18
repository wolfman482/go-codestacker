package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"fiber-postgres-docker/config"
	"fiber-postgres-docker/handlers"
	"fiber-postgres-docker/routes"

	"github.com/gofiber/fiber/v2"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/unidoc/unipdf/v3/common/license"
)

func main() {
	licenseKey := os.Getenv("UNIPDF_LICENSE_KEY")
	err := license.SetMeteredKey(licenseKey)
	if err != nil {
		log.Fatalf("Error setting license key: %v", err)
	}

	os.Setenv("TMPDIR", "/tmp")
	os.Setenv("HOME", "/tmp")

	app := fiber.New()

	dbHost := os.Getenv("DB_HOST")
	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")

	dbURL := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable", dbUser, dbPassword, dbHost, dbName)

	var db *sqlx.DB
	for {
		db, err = sqlx.Connect("postgres", dbURL)
		if err == nil {
			break
		}
		log.Printf("Failed to connect to the database: %v", err)
		time.Sleep(2 * time.Second)
	}

	config.Db = db

	minioEndpoint := os.Getenv("MINIO_ENDPOINT")
	minioAccessKey := os.Getenv("MINIO_ROOT_USER")
	minioSecretKey := os.Getenv("MINIO_ROOT_PASSWORD")

	config.MinioClient, err = minio.New(minioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioAccessKey, minioSecretKey, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatalf("Failed to connect to MinIO: %v", err)
	}

	err = config.MinioClient.MakeBucket(context.Background(), "pdfs", minio.MakeBucketOptions{Region: "us-east-1"})
	if err != nil {
		exists, errBucketExists := config.MinioClient.BucketExists(context.Background(), "pdfs")
		if errBucketExists == nil && exists {
			log.Printf("Bucket 'pdfs' already exists")
		} else {
			log.Fatalf("Failed to create bucket: %v", err)
		}
	}

	handlers.CreateTable()

	routes.SetupRoutes(app)

	log.Fatal(app.Listen(":3000"))
}
