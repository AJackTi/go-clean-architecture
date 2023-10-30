package aws

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go/service/s3"

	"github.com/AJackTi/go-clean-architecture/config"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type S3 struct {
	config   *config.Config
	uploader *s3manager.Uploader
	session  *session.Session
}

func New(cfg *config.Config) (*S3, error) {
	sess, err := session.NewSession(
		&aws.Config{
			Region: aws.String(cfg.Region),
			Credentials: credentials.NewStaticCredentials(
				cfg.KeyID,
				cfg.AccessKey,
				"",
			),
		})
	if err != nil {
		return nil, err
	}
	uploader := s3manager.NewUploader(sess)

	return &S3{
		config:   cfg,
		uploader: uploader,
		session:  sess,
	}, nil
}

func (s *S3) UploadFileJSON(input interface{}, folder string, level, tokenID int) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	reader := strings.NewReader(string(data))

	_, err = s.uploader.Upload(&s3manager.UploadInput{
		ACL:    aws.String(s.config.ACL),
		Body:   reader,
		Bucket: aws.String(s.config.Bucket),
		Key:    aws.String(fmt.Sprintf("%s/%d_%d.json", folder, level, tokenID)),
	})
	if err != nil {
		return err
	}

	return nil
}

func (s *S3) UploadFileImage(file multipart.File, fileHeader *multipart.FileHeader, level, tokenID int64, folder string) (string, error) {
	var (
		errInvalidInputFile = errors.New("the input file is invalid")
	)

	strSplit := strings.Split(fileHeader.Filename, ".")
	if len(strSplit) != 2 {
		return "", errInvalidInputFile
	}

	fileType := strSplit[len(strSplit)-1]
	size := fileHeader.Size
	buffer := make([]byte, size)
	_, err := file.Read(buffer)
	if err != nil {
		return "", err
	}

	fullFileName := fmt.Sprintf("%s/%d_%d.%s", folder, level, tokenID, fileType)

	if _, err := s3.New(s.session).PutObject(&s3.PutObjectInput{
		Bucket:        aws.String(s.config.Bucket),
		ACL:           aws.String(s.config.ACL),
		Key:           aws.String(fullFileName),
		Body:          bytes.NewReader(buffer),
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(http.DetectContentType(buffer)),
	}); err != nil {
		return "", err
	}

	return s.config.PathAvatar + fullFileName, nil
}

func (s *S3) UploadFile(buffer []byte, size int64, fileType, fileName, uploadToFolder string) (string, error) {
	fullFileName := fmt.Sprintf("%s/%s.%s", uploadToFolder, fileName, fileType)

	if _, err := s3.New(s.session).PutObject(&s3.PutObjectInput{
		Bucket:        aws.String(s.config.Bucket),
		ACL:           aws.String(s.config.ACL),
		Key:           aws.String(fullFileName),
		Body:          bytes.NewReader(buffer),
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(http.DetectContentType(buffer)),
	}); err != nil {
		return "", err
	}

	return s.config.PathAvatar + fullFileName, nil
}
