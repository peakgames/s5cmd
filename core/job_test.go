package core

import (
	"context"
	"io/ioutil"
	"os"
	"testing"

	"github.com/peak/s5cmd/objurl"
	"github.com/peak/s5cmd/op"
	"github.com/peak/s5cmd/opt"
	"github.com/peak/s5cmd/stats"
	"github.com/peak/s5cmd/storage"
)

func newJob(command string, operation op.Operation, opts opt.OptionList, dst *objurl.ObjectURL, src ...*objurl.ObjectURL) Job {
	return Job{
		command:   command,
		operation: operation,
		src:       src,
		opts:      opts,
		dst:       dst,
	}
}

func newURL(s string) *objurl.ObjectURL {
	url, _ := objurl.New(s)
	return url
}

var (
	st = stats.Stats{}
	wp = WorkerParams{
		ctx:        context.Background(),
		poolParams: nil,
		st:         &st,
		newClient: func(url *objurl.ObjectURL) (storage.Storage, error) {
			if url.IsRemote() {
				panic("remote url is not expected")
			}

			return storage.NewFilesystem(), nil
		}}

	// These Jobs are used for benchmarks and also as skeletons for tests
	localCopyJob = newJob(
		"!cp-test",
		op.LocalCopy,
		opt.OptionList{},
		newURL("test-dst"),
		newURL("test-src"),
	)

	localMoveJob = newJob(
		"!mv-test",
		op.LocalCopy,
		opt.OptionList{opt.DeleteSource},
		newURL("test-dst"),
		newURL("test-src"),
	)

	localDeleteJob = newJob(
		"!rm-test",
		op.LocalDelete,
		opt.OptionList{},
		nil,
		newURL("test-src"),
	)
)

func benchmarkJobRun(b *testing.B, j *Job) {

	for n := 0; n < b.N; n++ {
		createFile("test-src", "")
		j.Run(&wp)
	}

	deleteFile("test-dst")
}

func BenchmarkJobRunLocalCopy(b *testing.B) {
	benchmarkJobRun(b, &localCopyJob)
}

func BenchmarkJobRunLocalMove(b *testing.B) {
	benchmarkJobRun(b, &localMoveJob)
}

func BenchmarkJobRunLocalDelete(b *testing.B) {
	benchmarkJobRun(b, &localDeleteJob)
}

func createFile(filename, contents string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	f.WriteString(contents)
	return nil
}

func readFile(filename string) (string, error) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func deleteFile(filename string) {
	os.Remove(filename)
}

func tempFile(prefix string) (string, error) {
	f, err := ioutil.TempFile("", prefix)
	if err != nil {
		return "", err
	}
	filename := f.Name()
	f.Close()
	deleteFile(filename)

	return filename, nil
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

func TestJobRunLocalDelete(t *testing.T) {
	// setup
	filename, err := tempFile("localdelete")
	if err != nil {
		t.Fatal(err)
	}

	err = createFile(filename, "contents")
	if err != nil {
		t.Fatal(err)
	}
	defer deleteFile(filename)

	oldSrc := localDeleteJob.src
	oldDst := localDeleteJob.dst

	localDeleteJob.src = []*objurl.ObjectURL{newURL(filename)}

	// execute
	localDeleteJob.Run(&wp)

	// verify
	if fileExists(filename) {
		t.Error("File should not exist after delete")
	}

	localDeleteJob.src = oldSrc
	localDeleteJob.dst = oldDst
}

func testLocalCopyOrMove(t *testing.T, isMove bool) {
	// setup
	src, err := tempFile("src")
	if err != nil {
		t.Fatal(err)
	}
	fileContents := "contents"
	err = createFile(src, fileContents)
	if err != nil {
		t.Fatal(err)
	}

	var job *Job
	if isMove {
		job = &localMoveJob
	} else {
		job = &localCopyJob
	}

	oldSrc := job.src
	oldDst := job.dst
	dst := ""

	// teardown
	defer func() {
		deleteFile(src)
		if dst != "" {
			deleteFile(dst)
		}

		job.src = oldSrc
		job.dst = oldDst
	}()

	dst, err = tempFile("dst")
	if err != nil {
		t.Error(err)
		return
	}

	job.src = []*objurl.ObjectURL{newURL(src)}
	job.dst = newURL(dst)

	// execute
	job.Run(&wp)

	// verify
	if isMove {
		if fileExists(src) {
			t.Error("src should not exist after move")
			return
		}
	}

	newContents, err := readFile(dst)
	if err != nil {
		t.Error(err)
		return
	}

	if newContents != fileContents {
		t.Error("File contents do not match")
	}
}

func TestJobRunLocalCopy(t *testing.T) {
	testLocalCopyOrMove(t, false)
}

func TestJobRunLocalMove(t *testing.T) {
	testLocalCopyOrMove(t, true)
}
