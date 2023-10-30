// Package excel implements excel read.
package excel

import (
	"errors"
	"os"
	"strings"

	"github.com/xuri/excelize/v2"
)

const TotalRows = 5

// Excel -.
type Xlsx struct {
	*excelize.File
}

type MigrateOldUser struct {
	NewAddress string
	OldAddress string
}

func New(file string) (*Xlsx, error) {
	f, err := excelize.OpenFile(file)
	if err != nil {
		return nil, err
	}

	return &Xlsx{f}, nil
}

func readFileBuffer(filePath string) ([]byte, int64, error) {
	// check file is exist
	if _, err := os.Stat(filePath); errors.Is(err, os.ErrNotExist) {
		return nil, 0, err
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}

	filesize := fileInfo.Size()
	buffer := make([]byte, filesize)

	_, err = file.Read(buffer)
	if err != nil {
		return nil, 0, err
	}

	return buffer, filesize, nil
}

func listAllFiles(pathFolder string) ([]string, error) {
	files, err := os.ReadDir(pathFolder)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, f := range files {
		names = append(names, f.Name())
	}

	return names, nil
}

func findFile(files []string, fileName string) string {
	for _, el := range files {
		if strings.Split(el, ".")[0] == fileName {
			return el
		}
	}

	return ""
}
