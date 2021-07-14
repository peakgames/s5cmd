package storage

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/s3/s3manager/s3manageriface"
	"github.com/peak/s5cmd/log"
	"github.com/peak/s5cmd/storage/url"
)

var sentinelURL = urlpkg.URL{}

const (
	// deleteObjectsMax is the max allowed objects to be deleted on single HTTP
	// request.
	deleteObjectsMax = 1000

	// Amazon Accelerated Transfer endpoint
	transferAccelEndpoint = "s3-accelerate.amazonaws.com"

	// Google Cloud Storage endpoint
	gcsEndpoint = "storage.googleapis.com"
)

// Re-used AWS sessions dramatically improve performance.
var globalSessionCache = &SessionCache{
	sessions: map[Options]*session.Session{},
}

// S3 is a storage type which interacts with S3API, DownloaderAPI and
// UploaderAPI.
type S3 struct {
	api         s3iface.S3API
	downloader  s3manageriface.DownloaderAPI
	uploader    s3manageriface.UploaderAPI
	endpointURL urlpkg.URL
	dryRun      bool
}

func parseEndpoint(endpoint string) (urlpkg.URL, error) {
	if endpoint == "" {
		return sentinelURL, nil
	}
	// add a scheme to correctly parse the endpoint. Without a scheme,
	// url.Parse will put the host information in path"
	if !strings.HasPrefix(endpoint, "http") {
		endpoint = "http://" + endpoint
	}
	u, err := urlpkg.Parse(endpoint)
	if err != nil {
		return sentinelURL, fmt.Errorf("parse endpoint %q: %v", endpoint, err)
	}

	return *u, nil
}

// NewS3Storage creates new S3 session.
func newS3Storage(ctx context.Context, opts Options) (*S3, error) {
	endpointURL, err := parseEndpoint(opts.Endpoint)
	if err != nil {
		return nil, err
	}

	awsSession, err := globalSessionCache.newSession(ctx, opts)
	if err != nil {
		return nil, err
	}

	return &S3{
		api:         s3.New(awsSession),
		downloader:  s3manager.NewDownloader(awsSession),
		uploader:    s3manager.NewUploader(awsSession),
		endpointURL: endpointURL,
		dryRun:      opts.DryRun,
	}, nil
}

// Stat retrieves metadata from S3 object without returning the object itself.
func (s *S3) Stat(ctx context.Context, url *url.URL) (*Object, error) {
	output, err := s.api.HeadObjectWithContext(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(url.Bucket),
		Key:    aws.String(url.Path),
	})
	if err != nil {
		if errHasCode(err, "NotFound") {
			return nil, ErrGivenObjectNotFound
		}
		return nil, err
	}

	etag := aws.StringValue(output.ETag)
	mod := aws.TimeValue(output.LastModified)
	return &Object{
		URL:     url,
		Etag:    strings.Trim(etag, `"`),
		ModTime: &mod,
		Size:    aws.Int64Value(output.ContentLength),
	}, nil
}

// List is a non-blocking S3 list operation which paginates and filters S3
// keys. If no object found or an error is encountered during this period,
// it sends these errors to object channel.
func (s *S3) List(ctx context.Context, url *url.URL, _ bool) <-chan *Object {
	if isGoogleEndpoint(s.endpointURL) {
		return s.listObjects(ctx, url)
	}
	return s.listObjectsV2(ctx, url)
}

func (s *S3) listObjectsV2(ctx context.Context, url *url.URL) <-chan *Object {
	listInput := s3.ListObjectsV2Input{
		Bucket: aws.String(url.Bucket),
		Prefix: aws.String(url.Prefix),
	}

	if url.Delimiter != "" {
		listInput.SetDelimiter(url.Delimiter)
	}

	objCh := make(chan *Object)

	go func() {
		defer close(objCh)
		objectFound := false

		var now time.Time

		err := s.api.ListObjectsV2PagesWithContext(ctx, &listInput, func(p *s3.ListObjectsV2Output, lastPage bool) bool {
			for _, c := range p.CommonPrefixes {
				prefix := aws.StringValue(c.Prefix)
				if !url.Match(prefix) {
					continue
				}

				newurl := url.Clone()
				newurl.Path = prefix
				objCh <- &Object{
					URL:  newurl,
					Type: ObjectType{os.ModeDir},
				}

				objectFound = true
			}
			// track the instant object iteration began,
			// so it can be used to bypass objects created after this instant
			if now.IsZero() {
				now = time.Now().UTC()
			}

			for _, c := range p.Contents {
				key := aws.StringValue(c.Key)
				if !url.Match(key) {
					continue
				}

				mod := aws.TimeValue(c.LastModified).UTC()
				if mod.After(now) {
					objectFound = true
					continue
				}

				var objtype os.FileMode
				if strings.HasSuffix(key, "/") {
					objtype = os.ModeDir
				}

				newurl := url.Clone()
				newurl.Path = aws.StringValue(c.Key)
				etag := aws.StringValue(c.ETag)

				objCh <- &Object{
					URL:          newurl,
					Etag:         strings.Trim(etag, `"`),
					ModTime:      &mod,
					Type:         ObjectType{objtype},
					Size:         aws.Int64Value(c.Size),
					StorageClass: StorageClass(aws.StringValue(c.StorageClass)),
				}

				objectFound = true
			}

			return !lastPage
		})

		if err != nil {
			objCh <- &Object{Err: err}
			return
		}

		if !objectFound {
			objCh <- &Object{Err: ErrNoObjectFound}
		}
	}()

	return objCh
}

// listObjects is used for cloud services that does not support S3
// ListObjectsV2 API. I'm looking at you GCS.
func (s *S3) listObjects(ctx context.Context, url *url.URL) <-chan *Object {
	listInput := s3.ListObjectsInput{
		Bucket: aws.String(url.Bucket),
		Prefix: aws.String(url.Prefix),
	}

	if url.Delimiter != "" {
		listInput.SetDelimiter(url.Delimiter)
	}

	objCh := make(chan *Object)

	go func() {
		defer close(objCh)
		objectFound := false

		var now time.Time

		err := s.api.ListObjectsPagesWithContext(ctx, &listInput, func(p *s3.ListObjectsOutput, lastPage bool) bool {
			for _, c := range p.CommonPrefixes {
				prefix := aws.StringValue(c.Prefix)
				if !url.Match(prefix) {
					continue
				}

				newurl := url.Clone()
				newurl.Path = prefix
				objCh <- &Object{
					URL:  newurl,
					Type: ObjectType{os.ModeDir},
				}

				objectFound = true
			}
			// track the instant object iteration began,
			// so it can be used to bypass objects created after this instant
			if now.IsZero() {
				now = time.Now().UTC()
			}

			for _, c := range p.Contents {
				key := aws.StringValue(c.Key)
				if !url.Match(key) {
					continue
				}

				mod := aws.TimeValue(c.LastModified).UTC()
				if mod.After(now) {
					objectFound = true
					continue
				}

				var objtype os.FileMode
				if strings.HasSuffix(key, "/") {
					objtype = os.ModeDir
				}

				newurl := url.Clone()
				newurl.Path = aws.StringValue(c.Key)
				etag := aws.StringValue(c.ETag)

				objCh <- &Object{
					URL:          newurl,
					Etag:         strings.Trim(etag, `"`),
					ModTime:      &mod,
					Type:         ObjectType{objtype},
					Size:         aws.Int64Value(c.Size),
					StorageClass: StorageClass(aws.StringValue(c.StorageClass)),
				}

				objectFound = true
			}

			return !lastPage
		})

		if err != nil {
			objCh <- &Object{Err: err}
			return
		}

		if !objectFound {
			objCh <- &Object{Err: ErrNoObjectFound}
		}
	}()

	return objCh
}

// Copy is a single-object copy operation which copies objects to S3
// destination from another S3 source.
func (s *S3) Copy(ctx context.Context, from, to *url.URL, metadata Metadata) error {
	if s.dryRun {
		return nil
	}

	// SDK expects CopySource like "bucket[/key]"
	copySource := from.EscapedPath()

	input := &s3.CopyObjectInput{
		Bucket:     aws.String(to.Bucket),
		Key:        aws.String(to.Path),
		CopySource: aws.String(copySource),
	}

	storageClass := metadata.StorageClass()
	if storageClass != "" {
		input.StorageClass = aws.String(storageClass)
	}

	sseEncryption := metadata.SSE()
	if sseEncryption != "" {
		input.ServerSideEncryption = aws.String(sseEncryption)
		sseKmsKeyID := metadata.SSEKeyID()
		if sseKmsKeyID != "" {
			input.SSEKMSKeyId = aws.String(sseKmsKeyID)
		}
	}

	acl := metadata.ACL()
	if acl != "" {
		input.ACL = aws.String(acl)
	}

	cacheControl := metadata.CacheControl()
	if cacheControl != "" {
		input.CacheControl = aws.String(cacheControl)
	}

	expires := metadata.Expires()
	if expires != "" {
		t, err := time.Parse(time.RFC3339, expires)
		if err == nil {
			input.Expires = aws.Time(t)
		} else {
			msg := log.DebugMessage{Err: err.Error()}
			log.Debug(msg)
		}
	}

	_, err := s.api.CopyObject(input)
	return err
}

// Read fetches the remote object and returns its contents as an io.ReadCloser.
func (s *S3) Read(ctx context.Context, src *url.URL) (io.ReadCloser, error) {
	resp, err := s.api.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(src.Bucket),
		Key:    aws.String(src.Path),
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// Get is a multipart download operation which downloads S3 objects into any
// destination that implements io.WriterAt interface.
// Makes a single 'GetObject' call if 'concurrency' is 1 and ignores 'partSize'.
func (s *S3) Get(
	ctx context.Context,
	from *url.URL,
	to io.WriterAt,
	concurrency int,
	partSize int64,
) (int64, error) {
	if s.dryRun {
		return 0, nil
	}

	return s.downloader.DownloadWithContext(ctx, to, &s3.GetObjectInput{
		Bucket: aws.String(from.Bucket),
		Key:    aws.String(from.Path),
	}, func(u *s3manager.Downloader) {
		u.PartSize = partSize
		u.Concurrency = concurrency
	})
}

type SelectQuery struct {
	ExpressionType  string
	Expression      string
	CompressionType string
}

func (s *S3) Select(ctx context.Context, url *url.URL, query *SelectQuery, resultCh chan<- json.RawMessage) error {
	if s.dryRun {
		return nil
	}

	input := &s3.SelectObjectContentInput{
		Bucket:         aws.String(url.Bucket),
		Key:            aws.String(url.Path),
		ExpressionType: aws.String(query.ExpressionType),
		Expression:     aws.String(query.Expression),
		InputSerialization: &s3.InputSerialization{
			CompressionType: aws.String(query.CompressionType),
			JSON: &s3.JSONInput{
				Type: aws.String("Lines"),
			},
		},
		OutputSerialization: &s3.OutputSerialization{
			JSON: &s3.JSONOutput{},
		},
	}

	resp, err := s.api.SelectObjectContentWithContext(ctx, input)
	if err != nil {
		return err
	}

	reader, writer := io.Pipe()

	go func() {
		defer writer.Close()

		eventch := resp.EventStream.Reader.Events()
		defer resp.EventStream.Close()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-eventch:
				if !ok {
					return
				}

				switch e := event.(type) {
				case *s3.RecordsEvent:
					writer.Write(e.Payload)
				}
			}
		}
	}()

	decoder := json.NewDecoder(reader)
	for {
		var record json.RawMessage
		err := decoder.Decode(&record)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		resultCh <- record
	}

	return resp.EventStream.Reader.Err()
}

// Put is a multipart upload operation to upload resources, which implements
// io.Reader interface, into S3 destination.
func (s *S3) Put(
	ctx context.Context,
	reader io.Reader,
	to *url.URL,
	metadata Metadata,
	concurrency int,
	partSize int64,
) error {
	if s.dryRun {
		return nil
	}

	contentType := metadata.ContentType()
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	input := &s3manager.UploadInput{
		Bucket:      aws.String(to.Bucket),
		Key:         aws.String(to.Path),
		Body:        reader,
		ContentType: aws.String(contentType),
	}

	storageClass := metadata.StorageClass()
	if storageClass != "" {
		input.StorageClass = aws.String(storageClass)
	}
	acl := metadata.ACL()
	if acl != "" {
		input.ACL = aws.String(acl)
	}

	cacheControl := metadata.CacheControl()
	if cacheControl != "" {
		input.CacheControl = aws.String(cacheControl)
	}

	expires := metadata.Expires()
	if expires != "" {
		t, err := time.Parse(time.RFC3339, expires)
		if err == nil {
			input.Expires = aws.Time(t)
		} else {
			msg := log.DebugMessage{Err: err.Error()}
			log.Debug(msg)
		}
	}

	sseEncryption := metadata.SSE()
	if sseEncryption != "" {
		input.ServerSideEncryption = aws.String(sseEncryption)
		sseKmsKeyID := metadata.SSEKeyID()
		if sseKmsKeyID != "" {
			input.SSEKMSKeyId = aws.String(sseKmsKeyID)
		}
	}

	_, err := s.uploader.UploadWithContext(ctx, input, func(u *s3manager.Uploader) {
		u.PartSize = partSize
		u.Concurrency = concurrency
	})

	return err
}

// chunk is an object identifier container which is used on MultiDelete
// operations. Since DeleteObjects API allows deleting objects up to 1000,
// splitting keys into multiple chunks is required.
type chunk struct {
	Bucket string
	Keys   []*s3.ObjectIdentifier
}

// calculateChunks calculates chunks for given URL channel and returns
// read-only chunk channel.
func (s *S3) calculateChunks(ch <-chan *url.URL) <-chan chunk {
	chunkch := make(chan chunk)

	go func() {
		defer close(chunkch)

		var keys []*s3.ObjectIdentifier
		initKeys := func() {
			keys = make([]*s3.ObjectIdentifier, 0)
		}

		var bucket string
		for url := range ch {
			bucket = url.Bucket

			objid := &s3.ObjectIdentifier{Key: aws.String(url.Path)}
			keys = append(keys, objid)
			if len(keys) == deleteObjectsMax {
				chunkch <- chunk{
					Bucket: bucket,
					Keys:   keys,
				}
				initKeys()
			}
		}

		if len(keys) > 0 {
			chunkch <- chunk{
				Bucket: bucket,
				Keys:   keys,
			}
		}
	}()

	return chunkch
}

// Delete is a single object delete operation.
func (s *S3) Delete(ctx context.Context, url *url.URL) error {
	chunk := chunk{
		Bucket: url.Bucket,
		Keys: []*s3.ObjectIdentifier{
			{Key: aws.String(url.Path)},
		},
	}

	resultch := make(chan *Object, 1)
	defer close(resultch)

	s.doDelete(ctx, chunk, resultch)
	obj := <-resultch
	return obj.Err
}

// doDelete deletes the given keys given by chunk. Results are piggybacked via
// the Object container.
func (s *S3) doDelete(ctx context.Context, chunk chunk, resultch chan *Object) {
	if s.dryRun {
		for _, k := range chunk.Keys {
			key := fmt.Sprintf("s3://%v/%v", chunk.Bucket, aws.StringValue(k.Key))
			url, _ := url.New(key)
			resultch <- &Object{URL: url}
		}
		return
	}

	bucket := chunk.Bucket
	o, err := s.api.DeleteObjectsWithContext(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &s3.Delete{Objects: chunk.Keys},
	})
	if err != nil {
		resultch <- &Object{Err: err}
		return
	}

	for _, d := range o.Deleted {
		key := fmt.Sprintf("s3://%v/%v", bucket, aws.StringValue(d.Key))
		url, _ := url.New(key)
		resultch <- &Object{URL: url}
	}

	for _, e := range o.Errors {
		key := fmt.Sprintf("s3://%v/%v", bucket, aws.StringValue(e.Key))
		url, _ := url.New(key)
		resultch <- &Object{
			URL: url,
			Err: fmt.Errorf(aws.StringValue(e.Message)),
		}
	}
}

// MultiDelete is a asynchronous removal operation for multiple objects.
// It reads given url channel, creates multiple chunks and run these
// chunks in parallel. Each chunk may have at most 1000 objects since DeleteObjects
// API has a limitation.
// See: https://docs.aws.amazon.com/AmazonS3/latest/API/API_DeleteObjects.html.
func (s *S3) MultiDelete(ctx context.Context, urlch <-chan *url.URL) <-chan *Object {
	resultch := make(chan *Object)

	go func() {
		sem := make(chan bool, 10)
		defer close(sem)
		defer close(resultch)

		chunks := s.calculateChunks(urlch)

		var wg sync.WaitGroup
		for chunk := range chunks {
			chunk := chunk

			wg.Add(1)
			sem <- true

			go func() {
				defer wg.Done()
				s.doDelete(ctx, chunk, resultch)
				<-sem
			}()
		}

		wg.Wait()
	}()

	return resultch
}

// ListBuckets is a blocking list-operation which gets bucket list and returns
// the buckets that match with given prefix.
func (s *S3) ListBuckets(ctx context.Context, prefix string) ([]Bucket, error) {
	o, err := s.api.ListBucketsWithContext(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, err
	}

	var buckets []Bucket
	for _, b := range o.Buckets {
		bucketName := aws.StringValue(b.Name)
		if prefix == "" || strings.HasPrefix(bucketName, prefix) {
			buckets = append(buckets, Bucket{
				CreationDate: aws.TimeValue(b.CreationDate),
				Name:         bucketName,
			})
		}
	}
	return buckets, nil
}

// MakeBucket creates an S3 bucket with the given name.
func (s *S3) MakeBucket(ctx context.Context, name string) error {
	if s.dryRun {
		return nil
	}

	_, err := s.api.CreateBucketWithContext(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(name),
	})
	return err
}

// RemoveBucket removes an S3 bucket with the given name.
func (s *S3) RemoveBucket(ctx context.Context, name string) error {
	if s.dryRun {
		return nil
	}

	_, err := s.api.DeleteBucketWithContext(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(name),
	})
	return err
}

// SessionCache holds session.Session according to s3Opts and it synchronizes
// access/modification.
type SessionCache struct {
	sync.Mutex
	sessions map[Options]*session.Session
}

// newSession initializes a new AWS session with region fallback and custom
// options.
func (sc *SessionCache) newSession(ctx context.Context, opts Options) (*session.Session, error) {
	sc.Lock()
	defer sc.Unlock()

	if sess, ok := sc.sessions[opts]; ok {
		return sess, nil
	}

	awsCfg := aws.NewConfig()

	if opts.NoSignRequest {
		// do not sign requests when making service API calls
		awsCfg.Credentials = credentials.AnonymousCredentials
	}

	endpointURL, err := parseEndpoint(opts.Endpoint)
	if err != nil {
		return nil, err
	}

	// use virtual-host-style if the endpoint is known to support it,
	// otherwise use the path-style approach.
	isVirtualHostStyle := isVirtualHostStyle(endpointURL)

	useAccelerate := supportsTransferAcceleration(endpointURL)
	// AWS SDK handles transfer acceleration automatically. Setting the
	// Endpoint to a transfer acceleration endpoint would cause bucket
	// operations fail.
	if useAccelerate {
		endpointURL = sentinelURL
	}

	var httpClient *http.Client
	if opts.NoVerifySSL {
		httpClient = insecureHTTPClient
	}

	awsCfg = awsCfg.
		WithEndpoint(endpointURL.String()).
		WithS3ForcePathStyle(!isVirtualHostStyle).
		WithS3UseAccelerate(useAccelerate).
		WithHTTPClient(httpClient)

	awsCfg.Retryer = newCustomRetryer(opts.MaxRetries)

	useSharedConfig := session.SharedConfigEnable
	{
		// Reverse of what the SDK does: if AWS_SDK_LOAD_CONFIG is 0 (or a
		// falsy value) disable shared configs
		loadCfg := os.Getenv("AWS_SDK_LOAD_CONFIG")
		if loadCfg != "" {
			if enable, _ := strconv.ParseBool(loadCfg); !enable {
				useSharedConfig = session.SharedConfigDisable
			}
		}
	}

	sess, err := session.NewSessionWithOptions(
		session.Options{
			Config:            *awsCfg,
			SharedConfigState: useSharedConfig,
		},
	)
	if err != nil {
		return nil, err
	}

	// get region of the bucket and create session accordingly. if the region
	// is not provided, it means we want region-independent session
	// for operations such as listing buckets, making a new bucket etc.
	// only get bucket region when it is not specified.
	if opts.region != "" {
		sess.Config.Region = aws.String(opts.region)
	} else {
		if err := setSessionRegion(ctx, sess, opts.bucket); err != nil {
			return nil, err
		}
	}

	sc.sessions[opts] = sess

	return sess, nil
}

func (sc *SessionCache) clear() {
	sc.Lock()
	defer sc.Unlock()
	sc.sessions = map[Options]*session.Session{}
}

func setSessionRegion(ctx context.Context, sess *session.Session, bucket string) error {
	if aws.StringValue(sess.Config.Region) == "" {
		sess.Config.Region = aws.String(endpoints.UsEast1RegionID)
	}

	if bucket == "" {
		return nil
	}

	region, err := s3manager.GetBucketRegion(ctx, sess, bucket, "", func(r *request.Request) {
		r.Config.Credentials = sess.Config.Credentials
	})
	if err != nil {
		if errHasCode(err, "NotFound") {
			return err
		}
		// don't deny any request to the service if region auto-fetching
		// receives an error. Delegate error handling to command execution.
		err = fmt.Errorf("session: fetching region failed: %v", err)
		msg := log.ErrorMessage{Err: err.Error()}
		log.Error(msg)
	} else {
		sess.Config.Region = aws.String(region)
	}
	return nil
}

// customRetryer wraps the SDK's built in DefaultRetryer adding additional
// error codes. Such as, retry for S3 InternalError code.
type customRetryer struct {
	client.DefaultRetryer
}

func newCustomRetryer(maxRetries int) *customRetryer {
	return &customRetryer{
		DefaultRetryer: client.DefaultRetryer{
			NumMaxRetries: maxRetries,
		},
	}
}

// ShouldRetry overrides SDK's built in DefaultRetryer, adding custom retry
// logics that are not included in the SDK.
func (c *customRetryer) ShouldRetry(req *request.Request) bool {
	shouldRetry := errHasCode(req.Error, "InternalError") || errHasCode(req.Error, "RequestTimeTooSkewed") || strings.Contains(req.Error.Error(), "connection reset")
	if !shouldRetry {
		shouldRetry = c.DefaultRetryer.ShouldRetry(req)
	}

	if shouldRetry && req.Error != nil {
		err := fmt.Errorf("retryable error: %v", req.Error)
		msg := log.DebugMessage{Err: err.Error()}
		log.Debug(msg)
	}

	return shouldRetry
}

var insecureHTTPClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

func supportsTransferAcceleration(endpoint urlpkg.URL) bool {
	return endpoint.Hostname() == transferAccelEndpoint
}

func isGoogleEndpoint(endpoint urlpkg.URL) bool {
	return endpoint.Hostname() == gcsEndpoint
}

// isVirtualHostStyle reports whether the given endpoint supports S3 virtual
// host style bucket name resolving. If a custom S3 API compatible endpoint is
// given, resolve the bucketname from the URL path.
func isVirtualHostStyle(endpoint urlpkg.URL) bool {
	return endpoint == sentinelURL || supportsTransferAcceleration(endpoint) || isGoogleEndpoint(endpoint)
}

func errHasCode(err error, code string) bool {
	if err == nil || code == "" {
		return false
	}

	var awsErr awserr.Error
	if errors.As(err, &awsErr) {
		if awsErr.Code() == code {
			return true
		}
	}

	var multiUploadErr s3manager.MultiUploadFailure
	if errors.As(err, &multiUploadErr) {
		return errHasCode(multiUploadErr.OrigErr(), code)
	}

	return false

}

// IsCancelationError reports whether given error is a storage related
// cancelation error.
func IsCancelationError(err error) bool {
	return errHasCode(err, request.CanceledErrorCode)
}
