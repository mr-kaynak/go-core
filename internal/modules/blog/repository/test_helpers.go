package repository

import (
	"log"
	"os"

	"github.com/glebarez/sqlite"
	"github.com/mr-kaynak/go-core/internal/modules/blog/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SetupTestDB initializes an in-memory SQLite database and runs migrations for blog domain models.
func SetupTestDB() *gorm.DB {
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			LogLevel: logger.Silent, // Keep tests quiet
		},
	)

	// Use in-memory sqlite
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: newLogger,
	})
	if err != nil {
		panic("failed to connect database")
	}

	// Migrate the schemas specific to the blog module
	err = db.AutoMigrate(
		&domain.Category{},
		&domain.Tag{},
		&domain.Post{},
		&domain.PostTag{},
		&domain.PostRevision{},
		&domain.PostMedia{},
		&domain.PostStats{},
		&domain.PostView{},
		&domain.PostShare{},
		&domain.Comment{},
		&domain.PostLike{},
	)
	if err != nil {
		panic("failed to migrate database: " + err.Error())
	}

	return db
}
