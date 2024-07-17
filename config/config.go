package config

import (
	"github.com/jmoiron/sqlx"
	"github.com/minio/minio-go/v7"
)

var (
	Db          *sqlx.DB
	MinioClient *minio.Client
)
