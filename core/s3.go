package core

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/peak/s5cmd/s3url"
)

var (
	// ErrInterrupted is the error used when the main context is canceled
	ErrInterrupted = errors.New("operation interrupted")

	// ErrNilResult is returned if a nil result in encountered
	ErrNilResult = errors.New("nil result")
)

func s3delete(svc *s3.S3, obj *s3url.S3Url) (*s3.DeleteObjectOutput, error) {
	return svc.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(obj.Bucket),
		Key:    aws.String(obj.Key),
	})
}

func s3head(svc *s3.S3, obj *s3url.S3Url) (*s3.HeadObjectOutput, error) {
	return svc.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(obj.Bucket),
		Key:    aws.String(obj.Key),
	})
}

type s3listItem struct {
	*s3.Object

	key         string
	isDirectory bool
}

func s3list(ctx context.Context, svc *s3.S3, s3url *s3url.S3Url, emitChan chan<- interface{}) error {
	inp := s3.ListObjectsV2Input{
		Bucket: aws.String(s3url.Bucket),
		Prefix: aws.String(s3url.Prefix),
	}
	if s3url.Delimiter != "" {
		inp.SetDelimiter(s3url.Delimiter)
	}

	var mu sync.Mutex
	canceled := false
	isCanceled := func() bool {
		select {
		case <-ctx.Done():
			mu.Lock()
			defer mu.Unlock()
			canceled = true
			return true
		default:
			return false
		}
	}
	emit := func(item *s3listItem) bool {
		var data interface{}
		if item != nil {
			// avoid nil inside interface
			data = item
		}

		for {
			select {
			case <-ctx.Done():
				mu.Lock()
				defer mu.Unlock()
				canceled = true
				return false
			case emitChan <- data:
				return true
			}
		}
	}
	err := svc.ListObjectsV2PagesWithContext(ctx, &inp, func(p *s3.ListObjectsV2Output, lastPage bool) bool {
		if isCanceled() {
			return false
		}

		for _, c := range p.CommonPrefixes {
			key, ok := s3url.Match(*c.Prefix)
			if !ok {
				continue
			}

			if !emit(&s3listItem{
				Object:      &s3.Object{Key: c.Prefix},
				key:         key,
				isDirectory: true,
			}) {
				return false
			}
		}
		for _, c := range p.Contents {
			key, ok := s3url.Match(*c.Key)
			if !ok {
				continue
			}

			if !emit(&s3listItem{
				Object:      c,
				key:         key,
				isDirectory: key[len(key)-1] == '/',
			}) {
				return false
			}
		}
		if !*p.IsTruncated {
			emit(nil) // EOF
		}

		return !isCanceled()
	})

	mu.Lock()
	defer mu.Unlock()
	if err == nil && canceled {
		return ErrInterrupted
	}
	return err
}

type s3wildCallback func(*s3listItem) *Job

func s3wildOperation(url *s3url.S3Url, wp *WorkerParams, callback s3wildCallback) error {
	return wildOperation(wp, func(ch chan<- interface{}) error {
		return s3list(wp.ctx, wp.s3svc, url, ch)
	}, func(data interface{}) *Job {
		if data == nil {
			return callback(nil)
		}
		return callback(data.(*s3listItem))
	})
}

// NewAwsSession initializes a new AWS session with region fallback and custom options
func NewAwsSession(maxRetries int, endpointURL string, region string, noVerifySSL bool) (*session.Session, error) {
	newSession := func(c *aws.Config) (*session.Session, error) {
		useSharedConfig := session.SharedConfigEnable

		// Reverse of what the SDK does: if AWS_SDK_LOAD_CONFIG is 0 (or a falsy value) disable shared configs
		loadCfg := os.Getenv("AWS_SDK_LOAD_CONFIG")
		if loadCfg != "" {
			if enable, _ := strconv.ParseBool(loadCfg); !enable {
				useSharedConfig = session.SharedConfigDisable
			}
		}
		return session.NewSessionWithOptions(session.Options{Config: *c, SharedConfigState: useSharedConfig})
	}

	awsCfg := aws.NewConfig().WithMaxRetries(maxRetries) //.WithLogLevel(aws.LogDebug))

	if endpointURL != "" {
		awsCfg = awsCfg.WithEndpoint(endpointURL).WithS3ForcePathStyle(true)
		verboseLog("Setting Endpoint to %s on AWS Config", endpointURL)
	}

	if noVerifySSL {
		awsCfg = awsCfg.WithHTTPClient(&http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}})
	}

	if region != "" {
		awsCfg = awsCfg.WithRegion(region)
		return newSession(awsCfg)
	}

	ses, err := newSession(awsCfg)
	if err != nil {
		return nil, err
	}
	if (*ses).Config.Region == nil || *(*ses).Config.Region == "" { // No region specified in env or config, fallback to us-east-1
		awsCfg = awsCfg.WithRegion(endpoints.UsEast1RegionID)
		ses, err = newSession(awsCfg)
	}

	return ses, err
}

func GetSessionForBucket(svc *s3.S3, bucket string) (*session.Session, error) {
	o, err := svc.GetBucketLocation(&s3.GetBucketLocationInput{
		Bucket: &bucket,
	})
	if err == nil && o.LocationConstraint == nil {
		err = ErrNilResult
	}
	if err != nil {
		return nil, err
	}

	endpointURL := svc.Endpoint

	noVerifySSL := false
	transport, ok := svc.Config.HTTPClient.Transport.(*http.Transport)
	if ok {
		noVerifySSL = transport.TLSClientConfig.InsecureSkipVerify
	}

	return NewAwsSession(-1, endpointURL, *o.LocationConstraint, noVerifySSL)
}
