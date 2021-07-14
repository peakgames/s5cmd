package e2e

import (
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/fs"
	"gotest.tools/v3/icmd"
)

// ls
func TestListBuckets(t *testing.T) {
	t.Parallel()

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	// alphabetically unordered list of buckets
	bucketPrefix := s3BucketFromTestName(t)
	createBucket(t, s3client, bucketPrefix+"-1")
	createBucket(t, s3client, bucketPrefix+"-2")
	createBucket(t, s3client, bucketPrefix+"-4")
	createBucket(t, s3client, bucketPrefix+"-3")

	cmd := s5cmd("ls")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	// expect ordered list
	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix("s3://%v-1", bucketPrefix),
		1: suffix("s3://%v-2", bucketPrefix),
		2: suffix("s3://%v-3", bucketPrefix),
		3: suffix("s3://%v-4", bucketPrefix),
	})
}

// -json ls bucket
func TestListBucketsJSON(t *testing.T) {
	t.Parallel()

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	// alphabetically unordered list of buckets
	bucketPrefix := s3BucketFromTestName(t)
	createBucket(t, s3client, bucketPrefix+"-1")
	createBucket(t, s3client, bucketPrefix+"-2")
	createBucket(t, s3client, bucketPrefix+"-4")
	createBucket(t, s3client, bucketPrefix+"-3")

	cmd := s5cmd("--json", "ls")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	// expect ordered list
	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix(`"name":"%v-1"}`, bucketPrefix),
		1: suffix(`"name":"%v-2"}`, bucketPrefix),
		2: suffix(`"name":"%v-3"}`, bucketPrefix),
		3: suffix(`"name":"%v-4"}`, bucketPrefix),
	}, jsonCheck(true))
}

// ls bucket/object
func TestListSingleS3Object(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	// create 2 files, expect 1.
	putFile(t, s3client, bucket, "testfile1.txt", "this is a file content")
	putFile(t, s3client, bucket, "testfile2.txt", "this is also a file content")

	cmd := s5cmd("ls", "s3://"+bucket+"/testfile1.txt")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix("317 testfile1.txt"),
	})
}

// -json ls bucket/object
func TestListSingleS3ObjectJSON(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	// create 2 files, expect 1.
	putFile(t, s3client, bucket, "testfile1.txt", "this is a file content")
	putFile(t, s3client, bucket, "testfile2.txt", "this is also a file content")

	cmd := s5cmd("--json", "ls", "s3://"+bucket+"/testfile1.txt")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: prefix(`{"key":"s3://%v/testfile1.txt",`, bucket),
	}, jsonCheck(true))
}

// ls bucket/*.ext
func TestListSingleWildcardS3Object(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)
	putFile(t, s3client, bucket, "testfile1.txt", "this is a file content")
	putFile(t, s3client, bucket, "testfile2.txt", "this is also a file content")
	putFile(t, s3client, bucket, "testfile3.txt", "this is also a file content somehow")

	cmd := s5cmd("ls", "s3://"+bucket+"/*.txt")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix("317 testfile1.txt"),
		1: suffix("322 testfile2.txt"),
		2: suffix("330 testfile3.txt"),
	}, alignment(true))
}

// ls -s bucket/object
func TestListS3ObjectsWithDashS(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)
	putFile(t, s3client, bucket, "testfile1.txt", "this is a file content")

	cmd := s5cmd("ls", "-s", "s3://"+bucket+"/testfile1.txt")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	// TODO: test if full form of storage class is displayed (it can be done when and if gofakes3 supports storage classes)
}

// ls bucket/*/object*.ext
func TestListMultipleWildcardS3Object(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)
	putFile(t, s3client, bucket, "/a/testfile1.txt", "content")
	putFile(t, s3client, bucket, "/a/testfile2.txt", "content")
	putFile(t, s3client, bucket, "/b/testfile3.txt", "content")
	putFile(t, s3client, bucket, "/b/testfile4.txt", "content")
	putFile(t, s3client, bucket, "/c/testfile5.gz", "content")
	putFile(t, s3client, bucket, "/c/testfile6.txt.gz", "content")
	putFile(t, s3client, bucket, "/d/foo/bar/file7.txt", "content")
	putFile(t, s3client, bucket, "/d/foo/bar/testfile8.txt", "content")
	putFile(t, s3client, bucket, "/e/txt/testfile9.txt.gz", "content")
	putFile(t, s3client, bucket, "/f/txt/testfile10.txt", "content")

	const pattern = "/*/testfile*.txt"
	cmd := s5cmd("ls", "s3://"+bucket+pattern)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix("304 a/testfile1.txt"),
		1: suffix("304 a/testfile2.txt"),
		2: suffix("304 b/testfile3.txt"),
		3: suffix("304 b/testfile4.txt"),
		4: suffix("312 d/foo/bar/testfile8.txt"),
		5: suffix("309 f/txt/testfile10.txt"),
	}, alignment(true))
}

// ls bucket/prefix/object*.ext
func TestListMultipleWildcardS3ObjectWithPrefix(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)
	putFile(t, s3client, bucket, "/a/testfile1.txt", "content")
	putFile(t, s3client, bucket, "/a/testfile2.txt", "content")
	putFile(t, s3client, bucket, "/a/testfile3.txt", "content")
	putFile(t, s3client, bucket, "/b/testfile4.txt", "content")
	putFile(t, s3client, bucket, "/c/testfile5.gz", "content")
	putFile(t, s3client, bucket, "/c/testfile6.txt.gz", "content")
	putFile(t, s3client, bucket, "/d/foo/bar/file7.txt", "content")
	putFile(t, s3client, bucket, "/d/foo/bar/testfile8.txt", "content")
	putFile(t, s3client, bucket, "/e/txt/testfile9.txt.gz", "content")
	putFile(t, s3client, bucket, "/f/txt/testfile10.txt", "content")

	const pattern = "/a/testfile*.txt"
	cmd := s5cmd("ls", "s3://"+bucket+pattern)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix("304 testfile1.txt"),
		1: suffix("304 testfile2.txt"),
		2: suffix("304 testfile3.txt"),
	}, alignment(true))
}

// ls bucket
func TestListS3ObjectsAndFolders(t *testing.T) {
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

	cmd := s5cmd("ls", "s3://"+bucket)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix("DIR a/"),
		1: suffix("DIR b/"),
		2: suffix("DIR c/"),
		3: suffix("DIR d/"),
		4: suffix("DIR e/"),
		5: suffix("DIR f/"),
		6: suffix("298 report.gz"),
		7: suffix("302 testfile1.txt"),
	}, alignment(true))
}

// ls bucket/prefix
func TestListS3ObjectsAndFoldersWithPrefix(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)
	putFile(t, s3client, bucket, "testfile1.txt", "content")
	putFile(t, s3client, bucket, "report.gz", "content")
	putFile(t, s3client, bucket, "/a/testfile2.txt", "content")
	putFile(t, s3client, bucket, "/t/testfile3.txt", "content")

	// search with prefix t
	cmd := s5cmd("ls", "s3://"+bucket+"/t")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: suffix("DIR t/"),
		1: suffix("302 testfile1.txt"),
	}, alignment(true))
}

// ls bucket/*/object*.ext
func TestListNonexistingS3ObjectInGivenPrefix(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	const pattern = "/*/testfile*.txt"
	cmd := s5cmd("ls", "s3://"+bucket+pattern)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Expected{ExitCode: 1})

	assertLines(t, result.Stdout(), map[int]compareFunc{})

	assertLines(t, result.Stderr(), map[int]compareFunc{
		0: equals(`ERROR "ls s3://test-list-nonexisting-s-3-object-in-given-prefix/*/testfile*.txt": no object found`),
	}, strictLineCheck(false))
}

// ls bucket/object (nonexistent)
func TestListNonexistingS3Object(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	cmd := s5cmd("ls", "s3://"+bucket+"/nosuchobject")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Expected{ExitCode: 1})

	assertLines(t, result.Stdout(), map[int]compareFunc{})

	assertLines(t, result.Stderr(), map[int]compareFunc{
		0: equals(`ERROR "ls s3://%v/nosuchobject": no object found`, bucket),
	}, strictLineCheck(false))
}

// ls -e bucket
func TestListS3ObjectsWithDashE(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	putFile(t, s3client, bucket, "testfile1.txt", strings.Repeat("this is a file content", 10000))
	putFile(t, s3client, bucket, "testfile2.txt", strings.Repeat("this is also a file content", 10000))

	cmd := s5cmd("ls", "-e", "s3://"+bucket)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: match(`^ \w+ \d+ testfile1.txt$`),
		1: match(`^ \w+ \d+ testfile2.txt$`),
	}, trimMatch(dateRe), alignment(true))
}

// ls -H bucket
func TestListS3ObjectsWithDashH(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	putFile(t, s3client, bucket, "testfile1.txt", strings.Repeat("this is a file content", 10000))
	putFile(t, s3client, bucket, "testfile2.txt", strings.Repeat("this is also a file content", 10000))

	cmd := s5cmd("ls", "-H", "s3://"+bucket)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: match(`^ 215.1K testfile1.txt$`),
		1: match(`^ 264.0K testfile2.txt$`),
	}, trimMatch(dateRe), alignment(true))
}

// ls --exclude "main*" s3://bucket/
func TestListS3ObjectsWithExcludeFilter(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	const content = "content"
	const excludePattern = "*.txt"
	filenames := []string{
		"file.txt",
		"file.py",
		"hasan.txt",
		"a/try.txt",
		"a/try.py",
		"a/file.c",
		"file2.txt",
	}

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	for _, filename := range filenames {
		putFile(t, s3client, bucket, filename, content)

	}

	cmd := s5cmd("ls", "--exclude", excludePattern, "s3://"+bucket)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: match(`DIR a/`),
		1: match(`file.py`),
	}, trimMatch(dateRe), alignment(true))
}

// ls --exclude "" s3://bucket
func TestListS3ObjectsWithExcludeFilterEmpty(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	const content = "content"
	const excludePattern = ""
	filenames := []string{
		"file.txt",
		"file.py",
		"a/try.txt",
		"a/try.py",
		"a/file.c",
		"file2.txt",
	}

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	for _, filename := range filenames {
		putFile(t, s3client, bucket, filename, content)

	}

	cmd := s5cmd("ls", "--exclude", excludePattern, "s3://"+bucket)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: match(`DIR a/`),
		1: match(`file.py`),
		2: match("file.txt"),
		3: match("file2.txt"),
	}, trimMatch(dateRe), alignment(true))
}

// ls --exclude "main*" directory/
func TestListLocalFilesWithExcludeFilter(t *testing.T) {
	t.Parallel()

	_, s5cmd, cleanup := setup(t)
	defer cleanup()

	const excludePattern = "main*"
	folderLayout := []fs.PathOp{
		fs.WithDir(
			"main",
			fs.WithFile("try.txt", "this is a txt file"),
		),
		fs.WithFile("file1.txt", "this is the first test file"),
		fs.WithFile("main.py", "this is a python file"),
		fs.WithFile("main.c", "this is a c file"),
		fs.WithFile("main.txt", "this is a txt file"),
		fs.WithFile("main2.txt", "this is a txt file"),
		fs.WithFile("readme.md", "this is a readme file"),
	}

	workdir := fs.NewDir(t, t.Name(), folderLayout...)
	defer workdir.Remove()
	srcpath := workdir.Path()
	srcpath = filepath.ToSlash(srcpath)

	cmd := s5cmd("ls", "--exclude", excludePattern, srcpath)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: match("file1.txt"),
		1: match("readme.md"),
	}, trimMatch(dateRe), alignment(true))

}
