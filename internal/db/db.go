package db

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// PhotosDB wraps the database connection and manages temp file cleanup
type PhotosDB struct {
	db       *sql.DB
	tempDir  string
	tempPath string
}

// Open copies the Photos database to a temp location and opens it read-only.
// This is necessary because the database may be locked by Photos.app or other processes.
func Open(libraryPath string) (*PhotosDB, error) {
	srcPath := filepath.Join(libraryPath, "database", "Photos.sqlite")

	// Check if source exists
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("Photos database not found at %s", srcPath)
	}

	// Create temp directory
	tempDir, err := os.MkdirTemp("", "photoscli-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	// Copy the database file
	tempPath := filepath.Join(tempDir, "Photos.sqlite")
	if err := copyFile(srcPath, tempPath); err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("copy database: %w", err)
	}

	// Also copy WAL and SHM files if they exist (for consistency)
	copyFile(srcPath+"-wal", tempPath+"-wal")
	copyFile(srcPath+"-shm", tempPath+"-shm")

	// Open with read-only mode (don't use immutable with WAL databases)
	dsn := fmt.Sprintf("file:%s?mode=ro", tempPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		db.Close()
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &PhotosDB{
		db:       db,
		tempDir:  tempDir,
		tempPath: tempPath,
	}, nil
}

// Close cleans up the temp database and closes the connection
func (p *PhotosDB) Close() error {
	err := p.db.Close()
	os.RemoveAll(p.tempDir)
	return err
}

// DB returns the underlying sql.DB for queries
func (p *PhotosDB) DB() *sql.DB {
	return p.db
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
