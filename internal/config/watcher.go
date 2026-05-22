package config

import (
	"log"

	"github.com/fsnotify/fsnotify"
)

// Watcher 文件变更监听器
type Watcher struct {
	watcher  *fsnotify.Watcher
	done     chan struct{}
	callback func()
}

// NewWatcher 创建文件监听器
func NewWatcher(filePath string, callback func()) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := w.Add(filePath); err != nil {
		w.Close()
		return nil, err
	}

	wt := &Watcher{
		watcher:  w,
		done:     make(chan struct{}),
		callback: callback,
	}

	go wt.loop()
	return wt, nil
}

// loop 事件循环
func (w *Watcher) loop() {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			// 仅响应写入和创建事件
			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
				log.Printf("config file changed: %s", event.Name)
				w.callback()
			}
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("config watcher error: %v", err)
		case <-w.done:
			return
		}
	}
}

// Stop 停止监听
func (w *Watcher) Stop() {
	close(w.done)
	w.watcher.Close()
}
