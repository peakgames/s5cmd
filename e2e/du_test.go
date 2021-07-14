package e2e

import (
	"strings"
	"testing"

	"gotest.tools/v3/icmd"
)

func TestDiskUsageSingleS3Object(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	// create 2 files, expect 1.
	putFile(t, s3client, bucket, "testfile1.txt", "this is a file content")
	putFile(t, s3client, bucket, "testfile2.txt", "this is also a file content")

	cmd := s5cmd("du", "s3://"+bucket+"/testfile1.txt")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix(`317 bytes in 1 objects: s3://%v/testfile1.txt`, bucket),
	})
}

func TestDiskUsageSingleS3ObjectJSON(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	// create 2 files, expect 1.
	putFile(t, s3client, bucket, "testfile1.txt", "this is a file content")
	putFile(t, s3client, bucket, "testfile2.txt", "this is also a file content")

	cmd := s5cmd("--json", "du", "s3://"+bucket+"/testfile1.txt")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: json(`
			{
				"source": "s3://test-disk-usage-single-s-3-object-json/testfile1.txt",
				"count":1,
				"size":317
			}
		`),
	})
}

func TestDiskUsageMultipleS3Objects(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	putFile(t, s3client, bucket, "testfile1.txt", "this is a file content")
	putFile(t, s3client, bucket, "testfile2.txt", "this is also a file content")
	putFile(t, s3client, bucket, "foo/testfile3.txt", "this is also a file content")

	cmd := s5cmd("du", "s3://"+bucket)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix(`639 bytes in 2 objects: s3://%v`, bucket),
	})
}

func TestDiskUsageWildcard(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)
	putFile(t, s3client, bucket, "testfile1.txt", "this is a file content")
	putFile(t, s3client, bucket, "testfile2.txt", "this is also a file content")
	putFile(t, s3client, bucket, "foo/testfile3.txt", "this is also a file content somehow")
	putFile(t, s3client, bucket, "bar/testfile3.gz", "this is also a file content somehow")

	cmd := s5cmd("du", "s3://"+bucket+"/*.txt")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix(`973 bytes in 3 objects: s3://%v/*.txt`, bucket),
	})
}

func TestDiskUsageS3ObjectsAndFolders(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)
	putFile(t, s3client, bucket, "testfile1.txt", "content")
	putFile(t, s3client, bucket, "report.gz", "content")
	putFile(t, s3client, bucket, "/a/testfile2.txt", "content")
	putFile(t, s3client, bucket, "/b/testfile3.txt", "content")
	putFile(t, s3client, bucket, "/b/testfile4.txt", "content")
	putFile(t, s3client, bucket, "/c/testfile5.gz", "content")
	putFile(t, s3client, bucket, "/d/foo/bar/file7.txt", "content")
	putFile(t, s3client, bucket, "/d/foo/bar/testfile8.txt", "content")
	putFile(t, s3client, bucket, "/e/txt/testfile9.txt.gz", "content")
	putFile(t, s3client, bucket, "/f/txt/testfile10.txt", "content")

	cmd := s5cmd("du", "s3://"+bucket)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix(`600 bytes in 2 objects: s3://%v`, bucket),
	})
}

func TestDiskUsageWildcardS3ObjectsWithDashH(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	putFile(t, s3client, bucket, "testfile1.txt", strings.Repeat("this is a file content", 10000))
	putFile(t, s3client, bucket, "testfile2.txt", strings.Repeat("this is also a file content", 1000))
	putFile(t, s3client, bucket, "foo/testfile3.txt", strings.Repeat("this is also a file content", 1000))

	cmd := s5cmd("du", "-H", "s3://"+bucket)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix(`241.8K bytes in 2 objects: s3://%v`, bucket),
	})
}

func TestDiskUsageMissingObject(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	cmd := s5cmd("du", "s3://"+bucket+"/non-existent-file")
	result := icmd.RunCmd(cmd)

	// s5cmd returns 0 if given object is not found
	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix(`0 bytes in 0 objects: s3://%v/non-existent-file`, bucket),
	})
}

// du --exclude "main*" s3://bucket/*.txt
func TestDiskUsageWildcardWithExcludeFilter(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	const excludePattern = "main*"

	createBucket(t, s3client, bucket)
	putFile(t, s3client, bucket, "testfile1.txt", "this is a file content")
	putFile(t, s3client, bucket, "testfile2.txt", "this is also a file content")
	putFile(t, s3client, bucket, "main.txt", "this is also a file content")
	putFile(t, s3client, bucket, "main2.txt", "this is also a file content")
	putFile(t, s3client, bucket, "main.py", "this is a python file")
	putFile(t, s3client, bucket, "foo/testfile3.txt", "this is also a file content somehow")
	putFile(t, s3client, bucket, "bar/testfile3.gz", "this is also a file content somehow")

	cmd := s5cmd("du", "--exclude", excludePattern, "s3://"+bucket+"/*.txt")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix(`973 bytes in 3 objects: s3://%v/*.txt`, bucket),
	})
}
