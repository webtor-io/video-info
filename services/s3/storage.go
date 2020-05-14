package s3

import (
	"bytes"
	"io/ioutil"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	cs "github.com/webtor-io/common-services"

	"github.com/pkg/errors"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

type S3Storage struct {
	bucket string
	cl     *cs.S3Client
}

const (
	AWS_BUCKET = "aws-bucket"
	USE_S3     = "use-s3"
)

func RegisterS3StorageFlags(c *cli.App) {
	c.Flags = append(c.Flags, cli.StringFlag{
		Name:   AWS_BUCKET,
		Usage:  "AWS Bucket",
		Value:  "",
		EnvVar: "AWS_BUCKET",
	})
	c.Flags = append(c.Flags, cli.BoolFlag{
		Name:   USE_S3,
		Usage:  "Use S3",
		EnvVar: "USE_S3",
	})
}

func NewS3Storage(c *cli.Context, cl *cs.S3Client) *S3Storage {
	if !c.Bool(USE_S3) {
		return nil
	}
	return &S3Storage{
		bucket: c.String(AWS_BUCKET),
		cl:     cl,
	}
}

func (s *S3Storage) GetSub(id string) ([]byte, error) {
	key := "opensubtitles/" + id
	log.Infof("Fetching sub key=%v bucket=%v", key, s.bucket)
	r, err := s.cl.Get().GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == s3.ErrCodeNoSuchKey {
			return nil, nil
		}
		return nil, errors.Wrap(err, "Failed to fetch sub")
	}
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == s3.ErrCodeNoSuchKey {
			return nil, nil
		}
		return nil, errors.Wrap(err, "Failed to read sub")
	}
	return b, nil
}

func (s *S3Storage) PutSub(id string, data []byte) (err error) {
	key := "opensubtitles/" + id
	log.Infof("Storing sub key=%v bucket=%v", key, s.bucket)
	_, err = s.cl.Get().PutObject(&s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return errors.Wrap(err, "Failed to store sub")
	}
	return nil
}
