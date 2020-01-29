package e2e

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/google/go-cmp/cmp"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3bolt"
	"gotest.tools/v3/icmd"
)

var (
	defaultAccessKeyID     = "s5cmd-test-access-key-id"
	defaultSecretAccessKey = "s5cmd-test-secret-access-key"
)

func setup(t *testing.T, testname string) (*s3.S3, func(...string) icmd.Cmd, func()) {
	dbdir := filepath.Join(os.TempDir(), "s5cmd-test")
	if err := os.MkdirAll(dbdir, 0755); err != nil {
		t.Fatal(err)
	}

	tmpdir := filepath.Join(dbdir, testname, "tmp")
	if err := os.MkdirAll(tmpdir, 0755); err != nil {
		t.Fatal(err)
	}

	// we use boltdb as the s3 backend because listing buckets in in-memory
	// backend is not deterministic.
	dbpath := filepath.Join(dbdir, testname+".boltdb")
	backend, err := s3bolt.NewFile(dbpath)
	if err != nil {
		t.Fatal(err)
	}

	faker := gofakes3.New(backend)
	s3srv := httptest.NewServer(faker.Server())

	s3Config := &aws.Config{
		Credentials:      credentials.NewStaticCredentials("YOUR-ACCESSKEYID", "YOUR-SECRETACCESSKEY", ""),
		Endpoint:         aws.String(s3srv.URL),
		Region:           aws.String("us-east-1"),
		DisableSSL:       aws.Bool(true),
		S3ForcePathStyle: aws.Bool(true),
	}

	sess := session.New(s3Config)

	s5cmd := func(args ...string) icmd.Cmd {
		endpoint := []string{"-endpoint-url", s3srv.URL}
		args = append(endpoint, args...)

		cmd := icmd.Command("s5cmd", args...)
		cmd.Dir = tmpdir
		return cmd
	}

	cleanup := func() {
		os.Remove(dbpath)
		s3srv.Close()
	}

	return s3.New(sess), s5cmd, cleanup
}

func createBucket(t *testing.T, client *s3.S3, bucket string) {
	input := &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	}

	_, err := client.CreateBucket(input)
	if err != nil {
		t.Fatal(err)
	}
}

func putFile(t *testing.T, client *s3.S3, bucket string, filename string, content string) {
	// Upload a new object "testobject" with the string "Hello World!" to our "newbucket".
	_, err := client.PutObject(&s3.PutObjectInput{
		Body:   strings.NewReader(content),
		Bucket: aws.String(bucket),
		Key:    aws.String(filename),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func replaceMatchWithSpace(input string, match ...string) string {
	for _, m := range match {
		if m == "" {
			continue
		}
		re := regexp.MustCompile(m)
		input = re.ReplaceAllString(input, " ")
	}

	return input
}

type compareFunc func(string) error

func assert(t *testing.T, actual string, expectedlines map[int]compareFunc, strict bool) {
	t.Helper()

	lines := strings.Split(actual, "\n")

	for i, line := range lines {
		// trim consecutive spaces
		line = replaceMatchWithSpace(line, `\s+`)
		cmp, ok := expectedlines[i]
		if !ok {
			if strict {
				t.Fatalf("expected a comparison function for line %q (lineno: %v)", line, i)
			}
			continue
		}

		if err := cmp(line); err != nil {
			t.Errorf("line %v: %v", i, err)
		}
	}

	if t.Failed() {
		t.Log(actual)
	}
}

func match(expected string) compareFunc {
	re := regexp.MustCompile(expected)
	return func(actual string) error {
		if re.MatchString(actual) {
			return nil
		}
		return fmt.Errorf("match: given %q regex doesn't match with %q", expected, actual)
	}
}

func equals(expected string) compareFunc {
	return func(actual string) error {
		if expected == actual {
			return nil
		}

		diff := cmp.Diff(expected, actual)
		return fmt.Errorf("equals: (-want +got):\n%v", diff)
	}
}

func prefix(expected string) compareFunc {
	return func(actual string) error {
		if strings.HasPrefix(actual, expected) {
			return nil
		}

		diff := cmp.Diff(expected, actual)
		return fmt.Errorf("prefix: (-want +got):\n%v", diff)
	}
}

func suffix(expected string) compareFunc {
	return func(actual string) error {
		if strings.HasSuffix(actual, expected) {
			return nil
		}

		diff := cmp.Diff(expected, actual)
		return fmt.Errorf("suffix: (-want +got):\n%v", diff)
	}
}

func contains(expected string) compareFunc {
	return func(actual string) error {
		if strings.Contains(actual, expected) {
			return nil
		}

		diff := cmp.Diff(expected, actual)
		return fmt.Errorf("contains: (-want +got):\n%v", diff)
	}
}