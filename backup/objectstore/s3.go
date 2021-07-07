package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"

	"github.com/reddec/git-pipe/backup"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

const defaultRegion = "us-west-1"

func FromURL(u url.URL) *S3 {
	secret, _ := u.User.Password()
	pathStyle, _ := strconv.ParseBool(u.Query().Get("path_style"))
	region := u.Query().Get("region")
	if region == "" {
		region = defaultRegion
	}
	endpoint := u.Host
	disableSSL, _ := strconv.ParseBool(u.Query().Get("disable_ssl"))
	if disableSSL {
		endpoint = "http://" + endpoint
	}

	return &S3{
		Region:         region,
		Endpoint:       endpoint,
		ForcePathStyle: pathStyle,
		ID:             u.User.Username(),
		Secret:         secret,
		Bucket:         u.Path,
	}
}

type S3 struct {
	Region         string
	Endpoint       string
	ForcePathStyle bool
	ID             string
	Secret         string
	Bucket         string
}

func (ss *S3) Backup(ctx context.Context, name string, sourceFile string) error {
	s, err := ss.getSession()
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	f, err := os.Open(sourceFile)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer f.Close()

	svc := s3.New(s)
	_, err = svc.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Body:   f,
		Bucket: &ss.Bucket,
		Key:    &name,
	})
	if err != nil {
		return fmt.Errorf("upload file: %w", err)
	}

	return nil
}

func (ss *S3) Restore(ctx context.Context, name string, targetFile string) error {
	s, err := ss.getSession()
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	out, err := os.Create(targetFile)
	if err != nil {
		return fmt.Errorf("create target file: %w", err)
	}
	defer out.Close()

	svc := s3.New(s)

	res, err := svc.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: &ss.Bucket,
		Key:    &name,
	})

	var awsErr awserr.Error

	if errors.As(err, &awsErr) && awsErr.Code() == s3.ErrCodeNoSuchKey {
		return backup.ErrBackupNotExists
	}
	if err != nil {
		return fmt.Errorf("get object: %w", err)
	}

	defer res.Body.Close()

	_, err = io.Copy(out, res.Body)
	if err != nil {
		return fmt.Errorf("copy object: %w", err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}
	return nil
}

func (ss *S3) getSession() (*session.Session, error) {
	s, err := session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Credentials:      credentials.NewStaticCredentials(ss.ID, ss.Secret, ""),
			Endpoint:         &ss.Endpoint,
			Region:           &ss.Region,
			S3ForcePathStyle: &ss.ForcePathStyle,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create new session: %w", err)
	}
	return s, nil
}
