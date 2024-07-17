package main

import (
	"context"
	"log"
	"os"

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
	licenseKey := os.Getenv("7a997b47a356b861c27b727a1093104e55eace9eb185ccab3c8a463c3e1bb842")
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

	dbURL := "postgres://" + dbUser + ":" + dbPassword + "@" + dbHost + ":5432/" + dbName + "?sslmode=disable"
	config.Db, err = sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to the database: %v", err)
	}

	config.MinioClient, err = minio.New(os.Getenv("MINIO_ENDPOINT"), &minio.Options{
		Creds:  credentials.NewStaticV4(os.Getenv("MINIO_ROOT_USER"), os.Getenv("MINIO_ROOT_PASSWORD"), ""),
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
