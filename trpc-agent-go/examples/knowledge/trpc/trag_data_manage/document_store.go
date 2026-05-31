package main

import (
	"sync"
	"time"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

// DocumentRecord represents a document record in the in-memory database.
type DocumentRecord struct {
	ID          string
	DocID       string // Unique document ID stored in TRag metadata
	SourceName  string // Source name for grouping documents
	TraceID     string
	DocumentNum int
	ImportedAt  time.Time
	Metadata    map[string]any
}

// DocumentDataStore is an in-memory database for document tracking.
type DocumentDataStore struct {
	mu          sync.RWMutex
	records     map[string]*DocumentRecord // key: docID
	sourceIndex map[string][]string        // key: sourceName, value: []docID
}

// NewDocumentDataStore creates a new document data store.
func NewDocumentDataStore() *DocumentDataStore {
	return &DocumentDataStore{
		records:     make(map[string]*DocumentRecord),
		sourceIndex: make(map[string][]string),
	}
}

// Add adds a document record to the store.
func (ds *DocumentDataStore) Add(record *DocumentRecord) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.records[record.DocID] = record

	// Update source index
	if record.SourceName != "" {
		if _, exists := ds.sourceIndex[record.SourceName]; !exists {
			ds.sourceIndex[record.SourceName] = []string{}
		}
		ds.sourceIndex[record.SourceName] = append(ds.sourceIndex[record.SourceName], record.DocID)
	}
}

// RemoveBySource removes all documents for a given source name.
func (ds *DocumentDataStore) RemoveBySource(sourceName string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if docIDs, exists := ds.sourceIndex[sourceName]; exists {
		// Remove all documents from records
		for _, docID := range docIDs {
			delete(ds.records, docID)
		}
		// Remove from source index
		delete(ds.sourceIndex, sourceName)
	}
}

// Count returns the total number of records.
func (ds *DocumentDataStore) Count() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return len(ds.records)
}

// List returns all records.
func (ds *DocumentDataStore) List() []*DocumentRecord {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	records := make([]*DocumentRecord, 0, len(ds.records))
	for _, record := range ds.records {
		records = append(records, record)
	}
	return records
}
