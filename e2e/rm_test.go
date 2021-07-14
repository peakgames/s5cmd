package e2e

import (
	"fmt"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/fs"
	"gotest.tools/v3/icmd"
)

// rm s3://bucket/object
func TestRemoveSingleS3Object(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	const (
		filename = "testfile1.txt"
		content  = "this is a file content"
	)

	putFile(t, s3client, bucket, filename, content)

	cmd := s5cmd("rm", "s3://"+bucket+"/"+filename)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stderr(), map[int]compareFunc{})

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: equals("rm s3://%v/testfile1.txt", bucket),
	})

	// assert s3 object
	err := ensureS3Object(s3client, bucket, filename, content)
	assertError(t, err, errS3NoSuchKey)
}

// --json rm s3://bucket/object
func TestRemoveSingleS3ObjectJSON(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	const (
		filename = "testfile1.txt"
		content  = "this is a file content"
	)

	putFile(t, s3client, bucket, filename, content)

	cmd := s5cmd("--json", "rm", "s3://"+bucket+"/"+filename)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stderr(), map[int]compareFunc{})

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: json(`
			{
				"operation": "rm",
				"success": true,
				"source": "s3://%v/%v"
			}
		`, bucket, filename),
	})

	// assert s3 object
	err := ensureS3Object(s3client, bucket, filename, content)
	assertError(t, err, errS3NoSuchKey)
}

// rm s3://bucket/*
func TestRemoveMultipleS3Objects(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	filesToContent := map[string]string{
		"testfile1.txt":          "this is a test file 1",
		"readme.md":              "this is a readme file",
		"filename-with-hypen.gz": "file has hypen in its name",
		"another_test_file.txt":  "yet another txt file. yatf.",
	}

	for filename, content := range filesToContent {
		putFile(t, s3client, bucket, filename, content)
	}

	cmd := s5cmd("rm", "s3://"+bucket+"/*")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stderr(), map[int]compareFunc{})

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: equals(`rm s3://%v/another_test_file.txt`, bucket),
		1: equals(`rm s3://%v/filename-with-hypen.gz`, bucket),
		2: equals(`rm s3://%v/readme.md`, bucket),
		3: equals(`rm s3://%v/testfile1.txt`, bucket),
	}, sortInput(true))

	// assert s3 objects
	for filename, content := range filesToContent {
		err := ensureS3Object(s3client, bucket, filename, content)
		assertError(t, err, errS3NoSuchKey)
	}
}

// --json rm s3://bucket/*
func TestRemoveMultipleS3ObjectsJSON(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	filesToContent := map[string]string{
		"testfile1.txt":          "this is a test file 1",
		"readme.md":              "this is a readme file",
		"filename-with-hypen.gz": "file has hypen in its name",
		"another_test_file.txt":  "yet another txt file. yatf.",
	}

	for filename, content := range filesToContent {
		putFile(t, s3client, bucket, filename, content)
	}

	cmd := s5cmd("--json", "rm", "s3://"+bucket+"/*")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stderr(), map[int]compareFunc{})

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: json(`
			{
				"operation": "rm",
				"success": true,
				"source": "s3://%v/another_test_file.txt"
			}
		`, bucket),
		1: json(`
			{
				"operation": "rm",
				"success": true,
				"source": "s3://%v/filename-with-hypen.gz"
			}
		`, bucket),
		2: json(`
			{
				"operation": "rm",
				"success": true,
				"source": "s3://%v/readme.md"
			}
		`, bucket),
		3: json(`
			{
				"operation": "rm",
				"success": true,
				"source": "s3://%v/testfile1.txt"
			}
		`, bucket),
	}, sortInput(true))

	// assert s3 objects
	for filename, content := range filesToContent {
		err := ensureS3Object(s3client, bucket, filename, content)
		assertError(t, err, errS3NoSuchKey)
	}

}

// rm s3://bucket/* (removes 10k objects)
func TestRemoveTenThousandS3Objects(t *testing.T) {
	t.Parallel()

	// flaky test, skip it
	t.Skip()

	bucket := s3BucketFromTestName(t)

	// ten thousand s3 objects are created for this test. by default, s3 backend is
	// bolt but we need speed for this test, hence use in-memory storage.
	s3client, s5cmd, cleanup := setup(t, withS3Backend("mem"))
	defer cleanup()

	createBucket(t, s3client, bucket)

	const filecount = 10000

	filenameFunc := func(i int) string { return fmt.Sprintf("file_%06d", i) }
	contentFunc := func(i int) string { return fmt.Sprintf("file body %06d", i) }

	for i := 0; i < filecount; i++ {
		filename := filenameFunc(i)
		content := contentFunc(i)
		putFile(t, s3client, bucket, filename, content)
	}

	cmd := s5cmd("rm", "s3://"+bucket+"/*")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stderr(), map[int]compareFunc{})

	expected := make(map[int]compareFunc)
	for i := 0; i < filecount; i++ {
		expected[i] = contains(`rm s3://%v/file_%06d`, bucket, i)
	}

	assertLines(t, result.Stdout(), expected, sortInput(true))

	// assert s3 objects
	for i := 0; i < filecount; i++ {
		filename := filenameFunc(i)
		content := contentFunc(i)

		err := ensureS3Object(s3client, bucket, filename, content)
		assertError(t, err, errS3NoSuchKey)
	}
}

// rm s3://bucket/prefix
func TestRemoveS3PrefixWithoutSlash(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	const prefix = "prefix"
	src := fmt.Sprintf("s3://%v/%v", bucket, prefix)

	cmd := s5cmd("rm", src)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stderr(), map[int]compareFunc{})

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: equals("rm s3://%v/%v", bucket, prefix),
	})
}

// rm file
func TestRemoveSingleLocalFile(t *testing.T) {
	t.Parallel()

	_, s5cmd, cleanup := setup(t)
	defer cleanup()

	const (
		filename = "testfile1.txt"
		content  = "this is a test file"
	)

	workdir := fs.NewDir(t, t.Name(), fs.WithFile(filename, content))
	defer workdir.Remove()

	cmd := s5cmd("rm", filename)
	result := icmd.RunCmd(cmd, withWorkingDir(workdir))

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stderr(), map[int]compareFunc{})

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: equals(`rm %v`, filename),
	})

	// assert local filesystem
	expected := fs.Expected(t)
	assert.Assert(t, fs.Equal(workdir.Path(), expected))
}

// rm *.ext
func TestRemoveMultipleLocalFilesShouldNotFail(t *testing.T) {
	t.Parallel()

	_, s5cmd, cleanup := setup(t)
	defer cleanup()

	filesToContent := map[string]string{
		"testfile1.txt":          "this is a test file 1",
		"readme.md":              "this is a readme file",
		"filename-with-hypen.gz": "file has hypen in its name",
		"another_test_file.txt":  "yet another txt file. yatf.",
	}

	var files []fs.PathOp
	for filename, content := range filesToContent {
		op := fs.WithFile(filename, content)
		files = append(files, op)
	}

	workdir := fs.NewDir(t, t.Name(), files...)
	defer workdir.Remove()

	cmd := s5cmd("rm", "*.txt")
	result := icmd.RunCmd(cmd, withWorkingDir(workdir))

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: equals("rm another_test_file.txt"),
		1: equals("rm testfile1.txt"),
	}, sortInput(true))

	assertLines(t, result.Stderr(), map[int]compareFunc{})

	// assert local filesystem
	expectedFiles := []fs.PathOp{
		fs.WithFile("readme.md", "this is a readme file"),
		fs.WithFile("filename-with-hypen.gz", "file has hypen in its name"),
	}
	expected := fs.Expected(t, expectedFiles...)
	assert.Assert(t, fs.Equal(workdir.Path(), expected))
}

// rm dir/
func TestRemoveLocalDirectory(t *testing.T) {
	t.Parallel()

	_, s5cmd, cleanup := setup(t)
	defer cleanup()

	folderLayout := []fs.PathOp{
		fs.WithDir(
			"testdir",
			fs.WithFile("file1.txt", "this is the first test file"),
			fs.WithFile("file2.txt", "this is the second test file"),
			fs.WithFile("readme.md", "this is a readme file"),
		),
	}

	workdir := fs.NewDir(t, t.Name(), folderLayout...)
	defer workdir.Remove()

	cmd := s5cmd("rm", "testdir")
	result := icmd.RunCmd(cmd, withWorkingDir(workdir))

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: equals("rm testdir/file1.txt"),
		1: equals("rm testdir/file2.txt"),
		2: equals("rm testdir/readme.md"),
	}, sortInput(true))

	assertLines(t, result.Stderr(), map[int]compareFunc{})

	// expected empty dir
	expected := fs.Expected(t, fs.WithDir("testdir"))
	assert.Assert(t, fs.Equal(workdir.Path(), expected))
}

// rm prefix*
func TestRemoveLocalDirectoryWithGlob(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Files in Windows cannot contain glob(*) characters")
	}

	_, s5cmd, cleanup := setup(t)
	defer cleanup()

	folderLayout := []fs.PathOp{
		fs.WithDir(
			"abc*",
			fs.WithFile("file1.txt", "this is the first test file"),
			fs.WithFile("file2.txt", "this is the second test file"),
		),
		fs.WithDir(
			"abcd",
			fs.WithFile("file1.txt", "this is the first test file"),
		),
		fs.WithDir(
			"abcde",
			fs.WithFile("file1.txt", "this is the first test file"),
			fs.WithFile("file2.txt", "this is the second test file"),
		),
	}

	workdir := fs.NewDir(t, t.Name(), folderLayout...)
	defer workdir.Remove()

	cmd := s5cmd("rm", "abc*")
	result := icmd.RunCmd(cmd, withWorkingDir(workdir))

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: equals("rm abc*/file1.txt"),
		1: equals("rm abc*/file2.txt"),
		2: equals("rm abcd/file1.txt"),
		3: equals("rm abcde/file1.txt"),
		4: equals("rm abcde/file2.txt"),
	}, sortInput(true))

	assertLines(t, result.Stderr(), map[int]compareFunc{})

	// expected 3 empty dirs
	expected := fs.Expected(
		t,
		fs.WithDir("abc*"),
		fs.WithDir("abcd"),
		fs.WithDir("abcde"),
	)
	assert.Assert(t, fs.Equal(workdir.Path(), expected))
}

// rm dir/ file file2
func TestVariadicMultipleLocalFilesWithDirectory(t *testing.T) {
	t.Parallel()

	_, s5cmd, cleanup := setup(t)
	defer cleanup()

	folderLayout := []fs.PathOp{
		fs.WithDir(
			"testdir",
			fs.WithFile("readme.md", "this is a readme file"),
		),
		fs.WithFile("file1.txt", "this is the first test file"),
		fs.WithFile("file2.txt", "this is the second test file"),
	}

	workdir := fs.NewDir(t, t.Name(), folderLayout...)
	defer workdir.Remove()

	cmd := s5cmd("rm", "testdir", "file1.txt", "file2.txt")
	result := icmd.RunCmd(cmd, withWorkingDir(workdir))

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: equals("rm file1.txt"),
		1: equals("rm file2.txt"),
		2: equals("rm testdir/readme.md"),
	}, sortInput(true))

	assertLines(t, result.Stderr(), map[int]compareFunc{})

	// expected empty dir
	expected := fs.Expected(t, fs.WithDir("testdir"))
	assert.Assert(t, fs.Equal(workdir.Path(), expected))
}

// rm s3://bucket/object s3://bucket/object2
func TestVariadicRemoveS3Objects(t *testing.T) {
	t.Parallel()

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	bucket := s3BucketFromTestName(t)
	createBucket(t, s3client, bucket)

	filesToContent := map[string]string{
		"file1.txt": "file1 content",
		"file2.txt": "file2 content",
		"file3.txt": "file3 content",
	}

	for filename, content := range filesToContent {
		putFile(t, s3client, bucket, filename, content)
	}

	// file4.txt is non-existent. s5cmd sends DeleteObjects request but S3
	// doesn't report whether if the given object is exists, hence reported as
	// deleted. We want to keep this behaviour.
	cmd := s5cmd(
		"rm",
		"s3://"+bucket+"/file1.txt",
		"s3://"+bucket+"/file2.txt",
		"s3://"+bucket+"/file3.txt",
		"s3://"+bucket+"/file4.txt",
	)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stderr(), map[int]compareFunc{})

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: equals(`rm s3://%v/file1.txt`, bucket),
		1: equals(`rm s3://%v/file2.txt`, bucket),
		2: equals(`rm s3://%v/file3.txt`, bucket),
		3: equals(`rm s3://%v/file4.txt`, bucket),
	}, sortInput(true))

	// assert s3 objects
	for filename, content := range filesToContent {
		err := ensureS3Object(s3client, bucket, filename, content)
		assertError(t, err, errS3NoSuchKey)
	}
}

// rm s3://bucket/prefix/* s3://bucket/object
func TestVariadicRemoveS3ObjectsWithWildcard(t *testing.T) {
	t.Parallel()

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	bucket := s3BucketFromTestName(t)
	createBucket(t, s3client, bucket)

	filesToContent := map[string]string{
		"testdir1/file1.txt":     "file1 content",
		"testdir1/file2.txt":     "file2 content",
		"testdir1/dir/file3.txt": "file23content",
		"file4.txt":              "file4 content",
		"testdir2/file5.txt":     "file5 content",
		"testdir2/file6.txt":     "file6 content",
	}

	for filename, content := range filesToContent {
		putFile(t, s3client, bucket, filename, content)
	}

	cmd := s5cmd(
		"rm",
		"s3://"+bucket+"/testdir1/*",
		"s3://"+bucket+"/testdir2/*",
		"s3://"+bucket+"/file4.txt",
	)
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stderr(), map[int]compareFunc{})

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: equals(`rm s3://%v/file4.txt`, bucket),
		1: equals(`rm s3://%v/testdir1/dir/file3.txt`, bucket),
		2: equals(`rm s3://%v/testdir1/file1.txt`, bucket),
		3: equals(`rm s3://%v/testdir1/file2.txt`, bucket),
		4: equals(`rm s3://%v/testdir2/file5.txt`, bucket),
		5: equals(`rm s3://%v/testdir2/file6.txt`, bucket),
	}, sortInput(true))

	// assert s3 objects
	for filename, content := range filesToContent {
		err := ensureS3Object(s3client, bucket, filename, content)
		assertError(t, err, errS3NoSuchKey)
	}
}

// rm file s3://bucket/object
func TestRemoveMultipleMixedObjects(t *testing.T) {
	t.Parallel()

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	const bucket = "bucket"
	createBucket(t, s3client, bucket)

	const (
		filename = "file.txt"
		content  = "this is a test file"

		objectname = "object.txt"
	)

	workdir := fs.NewDir(t, t.Name(), fs.WithFile(filename, content))
	defer workdir.Remove()

	putFile(t, s3client, bucket, objectname, content)

	remoteSource := fmt.Sprintf("s3://%v/%v", bucket, objectname)

	cmd := s5cmd("rm", filename, remoteSource)
	result := icmd.RunCmd(cmd, withWorkingDir(workdir))

	result.Assert(t, icmd.Expected{ExitCode: 1})

	assertLines(t, result.Stderr(), map[int]compareFunc{
		0: equals(`ERROR "rm %v %v": arguments cannot have both local and remote sources`, filename, remoteSource),
	})

	// assert local filesystem
	expected := fs.Expected(t, fs.WithFile(filename, content))
	assert.Assert(t, fs.Equal(workdir.Path(), expected))

	// assert s3 object
	assert.Assert(t, ensureS3Object(s3client, bucket, objectname, content))
}

// --dry-run rm s3://bucket/*
func TestRemoveMultipleS3ObjectsDryRun(t *testing.T) {
	t.Parallel()

	bucket := s3BucketFromTestName(t)

	s3client, s5cmd, cleanup := setup(t)
	defer cleanup()

	createBucket(t, s3client, bucket)

	filesToContent := map[string]string{
		"testfile1.txt":          "this is a test file 1",
		"readme.md":              "this is a readme file",
		"filename-with-hypen.gz": "file has hypen in its name",
		"another_test_file.txt":  "yet another txt file. yatf.",
	}

	for filename, content := range filesToContent {
		putFile(t, s3client, bucket, filename, content)
	}

	cmd := s5cmd("--dry-run", "rm", "s3://"+bucket+"/*")
	result := icmd.RunCmd(cmd)

	result.Assert(t, icmd.Success)

	assertLines(t, result.Stderr(), map[int]compareFunc{})

	assertLines(t, result.Stdout(), map[int]compareFunc{
		0: equals(`rm s3://%v/another_test_file.txt`, bucket),
		1: equals(`rm s3://%v/filename-with-hypen.gz`, bucket),
		2: equals(`rm s3://%v/readme.md`, bucket),
		3: equals(`rm s3://%v/testfile1.txt`, bucket),
	}, sortInput(true))

	// assert s3 objects were not removed
	for filename, content := range filesToContent {
		assert.Assert(t, ensureS3Object(s3client, bucket, filename, content))
	}
}
