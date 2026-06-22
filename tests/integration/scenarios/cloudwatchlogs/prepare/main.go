// Command prepare builds the Lambda deployment package the scenario applies.
package main

import (
	"archive/zip"
	"log"
	"os"
)

const zipPath = "/tmp/unobin-it-cwl-lambda.zip"

const handlerSource = `def handler(event, context):
    return {"ok": True, "event": event}
`

func main() {
	if err := writeZip(); err != nil {
		log.Fatalf("prepare: %v", err)
	}
}

func writeZip() (err error) {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	zw := zip.NewWriter(f)
	header := &zip.FileHeader{Name: "index.py", Method: zip.Deflate}
	header.SetMode(0o644)
	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(handlerSource)); err != nil {
		return err
	}
	return zw.Close()
}
