package s3

import (
	"bytes"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	cs "github.com/webtor-io/common-services"
	"io"
	"strconv"

	"github.com/pkg/errors"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

type S3Storage struct {
	bucket string
	cl     *cs.S3Client
}

const (
	AwsBucketFlag = "aws-bucket"
	UseS3Flag     = "use-s3"
)

func RegisterS3StorageFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   AwsBucketFlag,
			Usage:  "AWS Bucket",
			Value:  "",
			EnvVar: "AWS_BUCKET",
		},
		cli.BoolFlag{
			Name:   UseS3Flag,
			Usage:  "Use S3",
			EnvVar: "USE_S3",
		},
	)
}

func NewS3Storage(c *cli.Context, cl *cs.S3Client) *S3Storage {
	if !c.Bool(UseS3Flag) {
		return nil
	}
	return &S3Storage{
		bucket: c.String(AwsBucketFlag),
		cl:     cl,
	}
}

func (s *S3Storage) GetSub(id int, format string) ([]byte, error) {
	key := "opensubtitles/" + strconv.Itoa(id) + "." + format
	log.Infof("fetching sub key=%v bucket=%v", key, s.bucket)
	r, err := s.cl.Get().GetObject(&s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == s3.ErrCodeNoSuchKey {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to fetch sub")
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == s3.ErrCodeNoSuchKey {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read sub")
	}
	return b, nil
}

func (s *S3Storage) PutSub(id int, format string, data []byte) (err error) {
	key := "opensubtitles/" + strconv.Itoa(id) + "." + format
	log.Infof("storing sub key=%v bucket=%v", key, s.bucket)
	_, err = s.cl.Get().PutObject(&s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return errors.Wrap(err, "failed to store sub")
	}
	return nil
}
