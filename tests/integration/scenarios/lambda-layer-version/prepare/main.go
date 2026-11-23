package main

import (
	"archive/zip"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
)

func main() {
	if err := run(filepath.Join(os.Getenv("SCENARIO_DIR"), "_output")); err != nil {
		log.Fatal(err)
	}
}

func run(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeArchive(filepath.Join(dir, "first.zip"), "Initial layer\n"); err != nil {
		return err
	}
	return writeArchive(filepath.Join(dir, "second.zip"), "Replacement layer content\n")
}

func writeArchive(path, content string) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	archive := zip.NewWriter(file)
	defer func() { err = errors.Join(err, archive.Close()) }()
	entry, err := archive.Create("python/layer.txt")
	if err != nil {
		return err
	}
	_, err = io.WriteString(entry, content)
	return err
}
