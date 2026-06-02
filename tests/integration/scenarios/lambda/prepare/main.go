// Command prepare builds the Lambda deployment package the scenario applies.
// The driver runs it before compiling the scenario, so the zip is in place by
// the time apply creates the function. It writes a one-file Python handler to a
// fixed absolute path the scenario's main.ub names, so the function resource
// reads it no matter which directory the driver applies from.
package main

import (
	"archive/zip"
	"log"
	"os"
)

// zipPath is the deployment package location. It is absolute so the function
// resource finds it from the driver's build directory, and it matches the
// zip-file-path the scenario's main.ub gives the function.
const zipPath = "/tmp/unobin-it-lambda.zip"

// handlerSource is a minimal Python handler. It echoes its event and a greeting
// read from the environment, enough for the invoke action to get a successful
// response carrying a JSON payload.
const handlerSource = `import os


def handler(event, context):
    return {"greeting": os.environ.get("GREETING", ""), "event": event}
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
