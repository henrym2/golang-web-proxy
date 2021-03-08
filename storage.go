package main

import (
	"net/http"
	"sync"
	"time"
)

//Item struct used for storing cache data
type Item struct {
	Content    http.Response
	Expiration int64
}

//Expired function for checking if the current entry has expired or not
func (item Item) Expired() bool {
	if item.Expiration == 0 {
		return false
	}
	return time.Now().UnixNano() > item.Expiration
}

//Storage struct used for storing Items
type Storage struct {
	items map[string]Item
	mutex *sync.RWMutex
}

//NewStorage Constructor
func NewStorage() *Storage {
	return &Storage{
		items: make(map[string]Item),
		mutex: &sync.RWMutex{},
	}
}

//Get - Getter for stored Items
func (s Storage) Get(key string) (*http.Response, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	item, prs := s.items[key]
	if item.Expired() {
		delete(s.items, key)
		return &http.Response{}, false
	} else if prs == false {
		return &http.Response{}, false
	}
	return &item.Content, true
}

//Store - Setter for stored items for a given duration
func (s Storage) Store(key string, content http.Response, duration time.Duration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.items[key] = Item{
		Content:    content,
		Expiration: time.Now().Add(duration).UnixNano(),
	}
}
