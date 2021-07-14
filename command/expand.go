package command

import (
	"context"
	"sync"

	"github.com/peak/s5cmd/storage"
	"github.com/peak/s5cmd/storage/url"
)

// expandSource returns the full list of objects from the given src argument.
// If src is an expandable URL, such as directory, prefix or a glob, all
// objects are returned by walking the source.
func expandSource(
	ctx context.Context,
	client storage.Storage,
	followSymlinks bool,
	srcurl *url.URL,
) (<-chan *storage.Object, error) {

	var isDir bool
	// if the source is local, we send a Stat call to know if  we have
	// directory or file to walk. For remote storage, we don't want to send
	// Stat since it doesn't have any folder semantics.
	if !srcurl.HasGlob() && !srcurl.IsRemote() {
		obj, err := client.Stat(ctx, srcurl)
		if err != nil {
			return nil, err
		}
		isDir = obj.Type.IsDir()
	}

	// call storage.List for only walking operations.
	if srcurl.HasGlob() || isDir {
		return client.List(ctx, srcurl, followSymlinks), nil
	}

	ch := make(chan *storage.Object, 1)
	if storage.ShouldProcessUrl(srcurl, followSymlinks) {
		ch <- &storage.Object{URL: srcurl}
	}
	close(ch)
	return ch, nil
}

// expandSources is a non-blocking argument dispatcher. It creates a object
// channel by walking and expanding the given source urls. If the url has a
// glob, it creates a goroutine to list storage items and sends them to object
// channel, otherwise it creates storage object from the original source.
func expandSources(
	ctx context.Context,
	client storage.Storage,
	followSymlinks bool,
	srcurls ...*url.URL,
) <-chan *storage.Object {
	ch := make(chan *storage.Object)

	go func() {
		defer close(ch)

		var wg sync.WaitGroup
		var objFound bool

		for _, origSrc := range srcurls {
			wg.Add(1)
			go func(origSrc *url.URL) {
				defer wg.Done()

				objch, err := expandSource(ctx, client, followSymlinks, origSrc)
				if err != nil {
					ch <- &storage.Object{Err: err}
					return
				}

				for object := range objch {
					if object.Err == storage.ErrNoObjectFound {
						continue
					}
					ch <- object
					objFound = true
				}
			}(origSrc)
		}

		wg.Wait()
		if !objFound {
			ch <- &storage.Object{Err: storage.ErrNoObjectFound}
		}
	}()

	return ch
}

// raw source returns the only object with given path.
func rawSource(srcurl *url.URL, followSymlinks bool) <-chan *storage.Object {
	ch := make(chan *storage.Object, 1)
	if storage.ShouldProcessUrl(srcurl, followSymlinks) {
		ch <- &storage.Object{URL: srcurl}
	}
	close(ch)
	return ch
}

// raw source returns the only object with given path.
func rawSourceUrls(srcurls []*url.URL, followSymlinks bool) <-chan *storage.Object {
	ch := make(chan *storage.Object, len(srcurls))
	for _, srcurl := range srcurls {
		if storage.ShouldProcessUrl(srcurl, followSymlinks) {
			ch <- &storage.Object{URL: srcurl}
		}
	}
	close(ch)
	return ch
}
