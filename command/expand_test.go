package command

import (
	"context"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"gotest.tools/v3/fs"

	"github.com/stretchr/testify/mock"

	"github.com/peak/s5cmd/storage"
	"github.com/peak/s5cmd/storage/url"
)

func TestExpandSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		src         map[string][]*storage.Object
		wantObjects []string
		wantError   error
	}{
		{
			name: "merge_multiple_source_urls",
			src: map[string][]*storage.Object{
				"s3://bucket/key": {
					{
						URL: &url.URL{
							Scheme: "s3",
							Bucket: "bucket",
							Path:   "key",
						},
					},
				},
				"s3://bucket/wildcard/*.txt": {
					{
						URL: &url.URL{
							Scheme: "s3",
							Bucket: "bucket",
							Path:   "wildcard/test.txt",
						},
					},
					{
						URL: &url.URL{
							Scheme: "s3",
							Bucket: "bucket",
							Path:   "wildcard/anothertest.txt",
						},
					},
				},
				"s3://bucket/dir/?/readme.md": {
					{
						URL: &url.URL{
							Scheme: "s3",
							Bucket: "bucket",
							Path:   "dir/a/readme.md",
						},
					},
					{
						URL: &url.URL{
							Scheme: "s3",
							Bucket: "bucket",
							Path:   "dir/b/readme.md",
						},
					},
				},
			},
			wantObjects: []string{
				"s3://bucket/dir/a/readme.md",
				"s3://bucket/dir/b/readme.md",
				"s3://bucket/key",
				"s3://bucket/wildcard/anothertest.txt",
				"s3://bucket/wildcard/test.txt",
			},
		},
		{
			name: "merge_multiple_with_empty_source",
			src: map[string][]*storage.Object{
				// this source has no item
				"s3://bucket/wildcard/*.txt": {
					{
						Err: storage.ErrNoObjectFound,
					},
				},
				"s3://bucket/*.txt": {
					{
						URL: &url.URL{
							Scheme: "s3",
							Bucket: "bucket",
							Path:   "file1.txt",
						},
					},
					{
						URL: &url.URL{
							Scheme: "s3",
							Bucket: "bucket",
							Path:   "file2.txt",
						},
					},
				},
			},
			wantObjects: []string{
				"s3://bucket/file1.txt",
				"s3://bucket/file2.txt",
			},
		},
		{
			// if multiple source has no item.
			// it will return single storage.ErrNoObjectFound error.
			name: "no_item_found",
			src: map[string][]*storage.Object{
				// this source has no item
				"s3://bucket/wildcard/*.txt": {
					{
						Err: storage.ErrNoObjectFound,
					},
				},
				"s3://bucket/*.txt": {
					{
						Err: storage.ErrNoObjectFound,
					},
				},
			},
			wantError: storage.ErrNoObjectFound,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// t.Parallel()

			srcurls, err := newURLs(keys(tc.src)...)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			client := &storage.MockStorage{}

			for src, objects := range tc.src {
				srcurl, err := url.New(src)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					continue
				}

				ch := generateObjects(objects)

				if src != "s3://bucket/key" {
					client.On("List", mock.Anything, srcurl, mock.Anything).Once().Return(ch)
				}
			}

			gotChan := expandSources(context.Background(), client, false, srcurls...)

			var objects []string
			for obj := range gotChan {
				if obj.Err != nil {
					if obj.Err != tc.wantError {
						t.Errorf("got error = %v, want %v", obj.Err, tc.wantError)
					}
					continue
				}
				objects = append(objects, obj.String())
			}
			// sort read objects
			sort.Strings(objects)
			if !reflect.DeepEqual(objects, tc.wantObjects) {
				t.Errorf("got = %v, want %v", objects, tc.wantObjects)
			}

			client.AssertExpectations(t)
		})
	}
}

func TestExpandSource_Follow_Link_To_Single_File(t *testing.T) {
	folderLayout := []fs.PathOp{
		fs.WithDir(
			"a",
			fs.WithFile("f1.txt", ""),
		),
		fs.WithDir(
			"b",
		),
		fs.WithSymlink("b/my_link", "a/f1.txt"),
	}

	workdir := fs.NewDir(t, "expandsourcetest", folderLayout...)
	defer workdir.Remove()

	ctx := context.Background()
	workdirUrl, _ := url.New(workdir.Join("b/my_link"))

	//follow symbolic links
	ch, _ := expandSource(ctx, storage.NewLocalClient(storage.Options{}), true, workdirUrl)
	var expected []string
	for obj := range ch {
		expected = append(expected, obj.URL.Absolute())
	}
	workdirJoin := filepath.ToSlash(workdir.Join("b/my_link"))
	assert.Equal(t, []string{workdirJoin}, expected)
}

func TestExpandSource_Do_Not_Follow_Link_To_Single_File(t *testing.T) {
	folderLayout := []fs.PathOp{
		fs.WithDir(
			"a",
			fs.WithFile("f1.txt", ""),
		),
		fs.WithDir(
			"b",
		),
		fs.WithSymlink("b/my_link", "a/f1.txt"),
	}

	workdir := fs.NewDir(t, "expandsourcetest", folderLayout...)
	defer workdir.Remove()

	ctx := context.Background()
	workdirUrl, _ := url.New(workdir.Join("b/my_link"))

	//do not follow symbolic links
	ch, _ := expandSource(ctx, storage.NewLocalClient(storage.Options{}), false, workdirUrl)
	var expected []string
	for obj := range ch {
		expected = append(expected, obj.URL.Absolute())
	}
	assert.Empty(t, expected)
}

func TestExpandSource_Follow_Link_To_Directory(t *testing.T) {
	folderLayout := []fs.PathOp{
		fs.WithDir(
			"a",
			fs.WithFile("f1.txt", ""),
			fs.WithFile("f2.txt", ""),
			fs.WithDir("b",
				fs.WithFile("f3.txt", "")),
		),
		fs.WithDir(
			"c",
		),
		fs.WithSymlink("c/my_link", "a"),
	}

	workdir := fs.NewDir(t, "expandsourcetest", folderLayout...)
	defer workdir.Remove()

	ctx := context.Background()
	workdirUrl, _ := url.New(workdir.Join("c/my_link"))

	//follow symbolic links
	ch, _ := expandSource(ctx, storage.NewLocalClient(storage.Options{}), true, workdirUrl)
	var expected []string
	for obj := range ch {
		expected = append(expected, obj.URL.Absolute())
	}
	sort.Strings(expected)
	assert.Equal(t, []string{
		filepath.ToSlash(workdir.Join("c/my_link/b/f3.txt")),
		filepath.ToSlash(workdir.Join("c/my_link/f1.txt")),
		filepath.ToSlash(workdir.Join("c/my_link/f2.txt")),
	}, expected)
}

func TestExpandSource_Do_Not_Follow_Link_To_Directory(t *testing.T) {
	folderLayout := []fs.PathOp{
		fs.WithDir(
			"a",
			fs.WithFile("f1.txt", ""),
			fs.WithFile("f2.txt", ""),
			fs.WithDir("b",
				fs.WithFile("f3.txt", "")),
		),
		fs.WithDir(
			"c",
		),
		fs.WithSymlink("c/my_link", "a"),
	}

	workdir := fs.NewDir(t, "expandsourcetest", folderLayout...)
	defer workdir.Remove()

	ctx := context.Background()
	workdirUrl, _ := url.New(workdir.Join("c/my_link"))

	//do not follow symbolic links
	ch, _ := expandSource(ctx, storage.NewLocalClient(storage.Options{}), false, workdirUrl)
	var expected []string
	for obj := range ch {
		expected = append(expected, obj.URL.Absolute())
	}
	assert.Empty(t, expected)
}

func TestExpandSource_Do_Not_Follow_Symlinks(t *testing.T) {
	ctx := context.Background()
	fileContent := "CAFEBABE"
	folderLayout := []fs.PathOp{
		fs.WithDir(
			"a",
			fs.WithFile("f1.txt", fileContent),
		),
		fs.WithDir("b"),
		fs.WithDir("c"),
		fs.WithSymlink("b/link1", "a/f1.txt"),
		fs.WithSymlink("c/link2", "b/link1"),
	}

	workdir := fs.NewDir(t, t.Name(), folderLayout...)
	defer workdir.Remove()

	workdirUrl, _ := url.New(workdir.Path())

	//do not follow symbolic links
	ch, _ := expandSource(ctx, storage.NewLocalClient(storage.Options{}), false, workdirUrl)
	var expected []string
	for obj := range ch {
		expected = append(expected, obj.URL.Absolute())
	}
	workdirJoin := filepath.ToSlash(workdir.Join("a/f1.txt"))
	assert.Equal(t, []string{workdirJoin}, expected)
}

func TestRawSourceWithoutSymLinks(t *testing.T) {

	if runtime.GOOS == "windows" {
		t.Skip()
	}
	fileContent := "CAFEBABE"
	folderLayout := []fs.PathOp{
		fs.WithDir(
			"a",
			fs.WithFile("f1*.txt", fileContent),
			fs.WithFile("f1*1.txt", fileContent),
		),
		fs.WithDir("b"),
		fs.WithDir("c"),
	}

	workdir := fs.NewDir(t, t.Name(), folderLayout...)
	defer workdir.Remove()

	workdirUrl, _ := url.New(workdir.Join("a", "f1*.txt"))

	//do not follow symbolic links
	ch := rawSource(workdirUrl, false)
	var expected []string
	for obj := range ch {
		expected = append(expected, obj.URL.Absolute())
	}
	workdirJoin := filepath.ToSlash(workdir.Join("a/f1*.txt"))
	assert.Equal(t, []string{workdirJoin}, expected)
}

func TestRawSourceUrlsWithoutSymLinks(t *testing.T) {

	if runtime.GOOS == "windows" {
		t.Skip()
	}
	fileContent := "CAFEBABE"
	folderLayout := []fs.PathOp{
		fs.WithDir(
			"a",
			fs.WithFile("f1*.txt", fileContent),
			fs.WithFile("f1*1.txt", fileContent),
		),
		fs.WithDir("b"),
		fs.WithDir("c"),
	}

	workdir := fs.NewDir(t, t.Name(), folderLayout...)
	defer workdir.Remove()

	workdirUrl, _ := url.New(workdir.Join("a", "f1*.txt"))
	secondWorkdirUrl, _ := url.New(workdir.Join("a", "f1*1.txt"))

	//do not follow symbolic links
	ch := rawSourceUrls([]*url.URL{workdirUrl, secondWorkdirUrl}, false)
	var expected []string
	for obj := range ch {
		expected = append(expected, obj.URL.Absolute())
	}
	workdirJoin := filepath.ToSlash(workdir.Join("a/f1*.txt"))
	secondWorkdirJoin := filepath.ToSlash(workdir.Join("a/f1*1.txt"))
	assert.Equal(t, []string{workdirJoin, secondWorkdirJoin}, expected)
}

func keys(urls map[string][]*storage.Object) []string {
	var urlKeys []string
	for key := range urls {
		urlKeys = append(urlKeys, key)
	}
	return urlKeys
}

func generateObjects(objects []*storage.Object) <-chan *storage.Object {
	ch := make(chan *storage.Object, len(objects))
	go func() {
		defer close(ch)
		for _, object := range objects {
			ch <- object
		}
	}()
	return ch
}
