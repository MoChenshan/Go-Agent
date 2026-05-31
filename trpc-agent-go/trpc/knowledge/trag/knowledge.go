// Package trag provides the knowledge base that uses TRag for semantic search.
package trag

import (
	"context"
	"fmt"
	"sync"
	"time"

	"git.woa.com/trag/trag-sdk/go-trag"
	tragretriever "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/retriever"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/sdk"
	tragsource "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/source"
	"golang.org/x/time/rate"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/embedder"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/query"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/reranker"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/reranker/topk"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	"trpc.group/trpc-go/trpc-agent-go/log"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc" // Import for side effects to register trpc components.
)

// ImportResult contains the result information of a document import operation.
type ImportResult struct {
	DocumentNum int
	TraceID     string
}

// ImportDocumentFunc is the function signature for importing a document.
type ImportDocumentFunc func(ctx context.Context, src source.Source, doc *document.Document) (*ImportResult, error)

// ImportDocumentHook is a middleware hook function for document import.
// It wraps the next function to enable pre/post processing.
type ImportDocumentHook func(next ImportDocumentFunc) ImportDocumentFunc

// Option is an option for the Knowledge instance.
type Option func(*Knowledge)

// Knowledge is a knowledge base that uses TRag for semantic search.
type Knowledge struct {
	tragOption sdk.TRagOption

	embedder              embedder.Embedder
	retriever             retriever.Retriever
	queryEnhancer         query.Enhancer
	reranker              reranker.Reranker
	sources               []source.Source
	rateLimiter           *rate.Limiter
	importDocumentHooks   []ImportDocumentHook
	disableRemoteChunking bool
}

// New creates a new Knowledge instance.
func New(opts ...Option) (*Knowledge, error) {
	k := &Knowledge{}

	for _, opt := range opts {
		opt(k)
	}

	if err := sdk.CheckTRagOption(&k.tragOption); err != nil {
		return nil, err
	}

	if k.tragOption.EmbeddingModel != "" && k.embedder != nil {
		log.Info("embedding model of TRag and embedder are both set, specified embedder will be used")
	}

	if k.retriever == nil {
		if k.reranker == nil {
			k.reranker = topk.New()
		}
		if k.queryEnhancer == nil {
			k.queryEnhancer = query.NewPassthroughEnhancer()
		}
		retriever, err := tragretriever.New(
			tragretriever.WithTRagOption(k.tragOption),
			tragretriever.WithEmbedder(k.embedder),
			tragretriever.WithReRanker(k.reranker),
			tragretriever.WithQueryEnhancer(k.queryEnhancer),
		)
		if err != nil {
			return nil, err
		}
		k.retriever = retriever
	}

	return k, nil
}

// LoadOption is an option for the Load method.
type LoadOption func(*loadOptions)

// WithSrcParallelism sets the source parallelism for the Load method.
func WithSrcParallelism(parallelism int) LoadOption {
	return func(o *loadOptions) {
		o.srcParallelism = parallelism
	}
}

// WithDocParallelism sets the document parallelism for the Load method.
func WithDocParallelism(parallelism int) LoadOption {
	return func(o *loadOptions) {
		o.docParallelism = parallelism
	}
}

// WithTRagRateLimit sets the rate limit for TRag ImportFiles calls.
// interval: minimum interval between requests (0 = no limit)
// burst: maximum burst size (0 = use default)
func WithTRagRateLimit(interval time.Duration, burst int) LoadOption {
	return func(o *loadOptions) {
		if interval < 0 {
			interval = 0 // Treat negative as no limit
		}
		if burst < 0 {
			burst = 0 // Treat negative as no limit
		}
		o.rateInterval = interval
		o.rateBurst = burst
	}
}

type loadOptions struct {
	srcParallelism int
	docParallelism int
	rateInterval   time.Duration
	rateBurst      int
}

// Load loads the knowledge source
func (dk *Knowledge) Load(ctx context.Context, opts ...LoadOption) error {
	config := &loadOptions{}
	for _, opt := range opts {
		opt(config)
	}

	// Set up rate limiter for ImportFiles calls if specified
	if config.rateInterval > 0 {
		// Create rate limiter with specified interval and burst
		burstSize := config.rateBurst
		if burstSize <= 0 {
			burstSize = 1 // Default burst size
		}
		dk.rateLimiter = rate.NewLimiter(rate.Every(config.rateInterval), burstSize)
	}

	// Derive automatic defaults when the caller did not specify explicit values.
	if config.srcParallelism == 0 {
		// Default to serial processing
		config.srcParallelism = 1
	} else if config.srcParallelism < 0 {
		return fmt.Errorf("srcParallelism cannot be negative: %d", config.srcParallelism)
	}

	if config.docParallelism == 0 {
		// Default to serial processing
		config.docParallelism = 1
	} else if config.docParallelism < 0 {
		return fmt.Errorf("docParallelism cannot be negative: %d", config.docParallelism)
	}

	// Timing variables.
	startTime := time.Now()
	totalSources := len(dk.sources)
	log.Infof("Starting knowledge base loading with %d sources", totalSources)

	// Use the concurrent loader when there is any real parallelism to gain.
	var err error
	if config.srcParallelism > 1 || config.docParallelism > 1 {
		err = dk.loadConcurrent(ctx, config)
	} else {
		for i := range dk.sources {
			if err = dk.loadSingleSrc(ctx, i, nil); err != nil {
				break
			}
		}
	}

	if err != nil {
		return err
	}

	elapsedTotal := time.Since(startTime)
	log.Infof("Knowledge base loading completed in %s (%d sources)",
		elapsedTotal, totalSources)
	return nil
}

func (dk *Knowledge) loadConcurrent(ctx context.Context, config *loadOptions) error {
	srcParallelism := max(config.srcParallelism, 1)
	docParallelism := max(config.docParallelism, 1)
	srcQueue := make(chan struct{}, srcParallelism)
	docQueue := make(chan struct{}, docParallelism)

	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error

	for i := range dk.sources {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			srcQueue <- struct{}{}
			defer func() {
				<-srcQueue
			}()

			if err := dk.loadSingleSrc(ctx, index, docQueue); err != nil {
				log.Errorf("Failed to load source %s: %v", dk.sources[index].Name(), err)
				errOnce.Do(func() {
					firstErr = err
				})
			}
		}(i)
	}
	wg.Wait()

	return firstErr
}

func (dk *Knowledge) loadSingleSrc(ctx context.Context, index int, docQueue chan struct{}) error {
	src := dk.sources[index]
	sourceName := src.Name()
	sourceType := src.Type()
	log.Infof("Loading source %d/%d: %s (type: %s)", index+1, len(dk.sources), sourceName, sourceType)

	docs, err := src.ReadDocuments(ctx)
	if err != nil {
		log.Errorf("Failed to read documents from source %s: %v", sourceName, err)
		return fmt.Errorf("failed to read documents from source %s: %w", sourceName, err)
	}
	log.Infof("Fetched %d document(s) from source %s", len(docs), sourceName)
	log.Infof("Start embedding & storing documents from source %s...", sourceName)
	// Process documents with progress logging if enabled.

	if docQueue == nil {
		// load docs in sequentially
		for j, doc := range docs {
			now := time.Now()
			err := dk.addDocument(ctx, src, doc)
			if err != nil {
				log.Errorf("Failed to add document from source %s: %v", sourceName, err)
				return fmt.Errorf("failed to add document from source %s: %w", sourceName, err)
			}
			cost := time.Since(now)
			log.Infof("Progress: %d/%d (%.2f%%) documents from source %s, cost: %s",
				j+1, len(docs), float64(j+1)/float64(len(docs))*100, sourceName, cost)
		}
	} else {
		// load docs in concurrently
		var wg sync.WaitGroup
		var errOnce sync.Once
		var firstErr error

		for j, doc := range docs {
			wg.Add(1)
			go func(index int, doc *document.Document) {
				defer wg.Done()

				docQueue <- struct{}{}
				defer func() {
					<-docQueue
				}()

				now := time.Now()
				err := dk.addDocument(ctx, src, doc)
				if err != nil {
					log.Errorf("Failed to add document from source %s: %v", sourceName, err)
					errOnce.Do(func() {
						firstErr = fmt.Errorf("failed to add document from source %s: %w", sourceName, err)
					})
					return
				}

				cost := time.Since(now)
				log.Infof("Progress: %d/%d (%.2f%%) documents from source %s processed, cost: %s",
					index+1, len(docs), float64(index+1)/float64(len(docs))*100, sourceName, cost)
			}(j, doc)
		}
		wg.Wait()

		if firstErr != nil {
			return firstErr
		}
	}

	log.Infof("Successfully loaded source %s", sourceName)
	return nil
}

func (dk *Knowledge) importDocument(ctx context.Context, src source.Source, req *document.Document) (*ImportResult, error) {
	if dk.disableRemoteChunking {
		return dk.importDocumentWithLocalChunking(ctx, src, req)
	}
	return dk.importDocumentWithRemoteChunking(ctx, src, req)
}

// importDocumentWithRemoteChunking uses TRag's ImportFile interface for remote chunking.
func (dk *Knowledge) importDocumentWithRemoteChunking(ctx context.Context, src source.Source, req *document.Document) (*ImportResult, error) {
	docType := dk.getDocumentType(src)

	// Convert document metadata to docKeyValue
	docKeyValue := make(map[string]any)
	if req.Metadata != nil {
		for k, v := range req.Metadata {
			docKeyValue[k] = v
		}
	}

	importFileReq := &trag.ImportFilesRequest{
		RagCode:        dk.tragOption.RagCode,
		NamespaceCode:  dk.tragOption.NamespaceCode,
		CollectionCode: dk.tragOption.CollectionCode,
		Name:           src.Name(),
		Files: []trag.ImportFile{
			{
				Type:    docType,
				Content: req.Content,
			},
		},
		Policy:      dk.tragOption.Policy,
		DocKeyValue: docKeyValue,
	}

	resp, err := dk.tragOption.Client.ImportFiles(ctx, importFileReq)
	if err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("import document failed: code %d, message: %s, trace: %s, name: %s, type: %s",
			resp.Code, resp.Message, resp.TraceID, req.Name, docType)
	}

	taskCode := resp.Data.ImportInfo.Code
	log.Infof("TRAG import task created: %s, task_code: %s, trace: %s",
		resp.Message, taskCode, resp.TraceID)

	result, err := dk.waitForImportComplete(ctx, taskCode)
	if err != nil {
		return result, fmt.Errorf("wait for import complete failed: %w", err)
	}

	return result, nil
}

// importDocumentWithLocalChunking uses TRag's ImportDocument interface for local chunking.
// This is a synchronous operation and does not require polling for task completion.
func (dk *Knowledge) importDocumentWithLocalChunking(ctx context.Context, _ source.Source, req *document.Document) (*ImportResult, error) {
	// Convert document metadata to docKeyValue
	docKeyValue := make(map[string]any)
	if req.Metadata != nil {
		for k, v := range req.Metadata {
			docKeyValue[k] = v
		}
	}
	fmt.Println("for test loglog", len(req.Content), req.ID)

	importDocReq := &trag.ImportDocumentRequest{
		RagCode:        dk.tragOption.RagCode,
		NamespaceCode:  dk.tragOption.NamespaceCode,
		CollectionCode: dk.tragOption.CollectionCode,
		EmbeddingModel: dk.tragOption.EmbeddingModel,
		Documents: []struct {
			ID             string         `json:"id"`
			Vector         []float64      `json:"vector,omitempty"`
			EmbeddingQuery string         `json:"embeddingQuery,omitempty"`
			Doc            string         `json:"doc"`
			DocKeyValue    map[string]any `json:"docKeyValue,omitempty"`
		}{
			{
				ID:             req.ID,
				Doc:            req.Content,
				EmbeddingQuery: req.Content,
				DocKeyValue:    docKeyValue,
			},
		},
	}

	resp, err := dk.tragOption.Client.ImportDocumentRequest(ctx, importDocReq)
	if err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("import document failed: code %d, message: %s, trace: %s, name: %s",
			resp.Code, resp.Message, resp.TraceID, req.Name)
	}

	log.Infof("TRAG document imported successfully (local chunking): %s, trace: %s", resp.Message, resp.TraceID)

	// ImportDocumentRequest is synchronous, return success immediately
	result := &ImportResult{
		DocumentNum: 1,
		TraceID:     resp.TraceID,
	}

	return result, nil
}

// waitForImportComplete polls the import task status until completion
func (dk *Knowledge) waitForImportComplete(ctx context.Context, taskCode string) (*ImportResult, error) {
	const (
		maxRetries   = 5 * 60
		pollInterval = 2 * time.Second
		stateSuccess = "Success"
		stateFailure = "Failure"
		stateLoading = "Loading"
	)

	for i := 0; i < maxRetries; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		queryReq := &trag.QueryImportStateRequest{
			Code: taskCode,
		}

		queryResp, err := dk.tragOption.Client.QueryImportState(ctx, queryReq)
		if err != nil {
			return nil, fmt.Errorf("failed to query import state: %w", err)
		}

		if queryResp.Code != 0 {
			return nil, fmt.Errorf("query import state failed: code=%d, message=%s, trace=%s",
				queryResp.Code, queryResp.Message, queryResp.TraceID)
		}

		importState := queryResp.Data.ImportInfo.ImportState
		result := &ImportResult{
			DocumentNum: queryResp.Data.ImportInfo.DocumentNum,
			TraceID:     queryResp.TraceID,
		}

		switch importState {
		case stateSuccess:
			log.Infof("TRAG import task completed successfully: task_code=%s, documents=%d, trace=%s",
				taskCode, result.DocumentNum, result.TraceID)
			return result, nil

		case stateFailure:
			return result, fmt.Errorf("import task failed: task_code=%s, trace=%s", taskCode, result.TraceID)

		case stateLoading:
			log.Debugf("Import task still loading: task_code=%s, attempt=%d/%d",
				taskCode, i+1, maxRetries)
			time.Sleep(pollInterval)

		default:
			return result, fmt.Errorf("unknown import state '%s' for task_code=%s, trace=%s",
				importState, taskCode, result.TraceID)
		}
	}

	return &ImportResult{}, fmt.Errorf("import task timeout after %d attempts: task_code=%s", maxRetries, taskCode)
}

// getDocumentType determines the document type for TRag import.
// For TRag-specific sources (trag_url, trag_file, trag_directory, trag_text), use their type directly.
// For standard sources, default to "text" type.
func (dk *Knowledge) getDocumentType(src source.Source) string {
	srcType := src.Type()
	switch srcType {
	case tragsource.TypeTRAGURL:
		return source.TypeURL
	case "trag_file", "trag_directory", "trag_text":
		return "text"
	default:
		return "text"
	}
}

// addDocument adds a document to the knowledge base (internal method).
func (dk *Knowledge) addDocument(ctx context.Context, src source.Source, doc *document.Document) error {
	// Use rate limiter if configured
	if dk.rateLimiter != nil {
		if err := dk.rateLimiter.Wait(ctx); err != nil {
			return err
		}
	}

	// Build the execution chain with hooks
	handler := dk.importDocument
	for i := len(dk.importDocumentHooks) - 1; i >= 0; i-- {
		handler = dk.importDocumentHooks[i](handler)
	}

	_, err := handler(ctx, src, doc)
	return err
}

// Search performs semantic search and returns the best result.
// This is the main method used by agents for RAG.
// Context includes conversation history for better search results.
func (dk *Knowledge) Search(ctx context.Context, req *knowledge.SearchRequest) (*knowledge.SearchResult, error) {
	if dk.retriever == nil {
		return nil, fmt.Errorf("retriever not configured")
	}

	minScore := req.MinScore
	if minScore < 0 {
		minScore = 0.0
	}

	var queryFilter *retriever.QueryFilter
	if req.SearchFilter != nil {
		queryFilter = &retriever.QueryFilter{
			DocumentIDs:     req.SearchFilter.DocumentIDs,
			Metadata:        req.SearchFilter.Metadata,
			FilterCondition: req.SearchFilter.FilterCondition,
		}
	}

	// Use built-in retriever for RAG pipeline.
	retrieverReq := &retriever.Query{
		Text:     req.Query,
		Limit:    req.MaxResults,
		MinScore: minScore,
		Filter:   queryFilter,
	}

	result, err := dk.retriever.Retrieve(ctx, retrieverReq)
	if err != nil {
		return nil, fmt.Errorf("retrieval failed: %w", err)
	}

	if len(result.Documents) == 0 {
		return nil, fmt.Errorf("no relevant documents found")
	}

	// Return the best result.
	bestDoc := result.Documents[0]
	content := bestDoc.Document.Content
	documents := make([]*knowledge.Result, len(result.Documents))
	for i, doc := range result.Documents {
		documents[i] = &knowledge.Result{
			Document: doc.Document,
			Score:    doc.Score,
		}
	}

	return &knowledge.SearchResult{
		Document:  bestDoc.Document,
		Score:     bestDoc.Score,
		Text:      content,
		Documents: documents,
	}, nil
}

// DeleteOption is an option for delete operations.
type DeleteOption func(*deleteOptions)

type deleteOptions struct {
	filterExpr  string
	documentIDs []string
}

// WithFilterExpr sets the filter expression for deletion.
// The filter expression supports metadata filtering using TRag's filter syntax.
//
// Example filter expressions:
//   - "source_type == 'trag_file'"
//   - "category == 'documentation' && type == 'guide'"
//   - "import_timestamp > 1701234567"
//   - "custom_id in ['id1', 'id2', 'id3']"
func WithFilterExpr(filterExpr string) DeleteOption {
	return func(o *deleteOptions) {
		o.filterExpr = filterExpr
	}
}

// WithDocumentIDs sets the document IDs for deletion.
func WithDocumentIDs(documentIDs []string) DeleteOption {
	return func(o *deleteOptions) {
		o.documentIDs = documentIDs
	}
}

// Delete deletes documents based on the provided options.
// You can delete by filter expression or by document IDs.
//
// Example usage:
//   - Delete by filter: Delete(ctx, WithFilterExpr("source_type == 'trag_file'"))
//   - Delete by IDs: Delete(ctx, WithDocumentIDs([]string{"id1", "id2"}))
//
// Returns the number of documents deleted.
func (dk *Knowledge) Delete(ctx context.Context, opts ...DeleteOption) (int, error) {
	options := &deleteOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if options.filterExpr == "" && len(options.documentIDs) == 0 {
		return 0, fmt.Errorf("either filter expression or document IDs must be provided")
	}

	if options.filterExpr != "" && len(options.documentIDs) > 0 {
		return 0, fmt.Errorf("cannot specify both filter expression and document IDs")
	}

	req := &trag.DeleteDocumentRequest{
		RagCode:        dk.tragOption.RagCode,
		NamespaceCode:  dk.tragOption.NamespaceCode,
		CollectionCode: dk.tragOption.CollectionCode,
	}

	if options.filterExpr != "" {
		req.FilterExpr = options.filterExpr
	} else {
		req.DocumentIds = options.documentIDs
	}

	resp, err := dk.tragOption.Client.DeleteDocumentRequest(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("delete document request failed: %w", err)
	}

	if resp.Code != 0 {
		return 0, fmt.Errorf("delete document failed: code=%d, message=%s, trace=%s",
			resp.Code, resp.Message, resp.TraceID)
	}

	deletedCount := int(resp.Data)

	if options.filterExpr != "" {
		log.Infof("Successfully deleted %d documents with filter: %s (trace: %s)",
			deletedCount, options.filterExpr, resp.TraceID)
	} else {
		log.Infof("Successfully deleted %d documents by IDs: %v (trace: %s)",
			deletedCount, options.documentIDs, resp.TraceID)
	}

	return deletedCount, nil
}

// convertConversationHistory converts conversation messages to query format.
func convertConversationHistory(history []knowledge.ConversationMessage) []query.ConversationMessage {
	result := make([]query.ConversationMessage, len(history))
	for i, msg := range history {
		result[i] = query.ConversationMessage(msg)
	}
	return result
}

// WithTRagOption set the trag client options
func WithTRagOption(opt sdk.TRagOption) Option {
	return func(k *Knowledge) {
		k.tragOption = opt
	}
}

// WithEmbedder sets the embedder for the Knowledge instance.
func WithEmbedder(e embedder.Embedder) Option {
	return func(k *Knowledge) {
		k.embedder = e
	}
}

// WithRetriever sets the retriever for the Knowledge instance.
func WithRetriever(r retriever.Retriever) Option {
	return func(k *Knowledge) {
		k.retriever = r
	}
}

// WithQueryEnhancer sets the query enhancer for the Knowledge instance.
func WithQueryEnhancer(qe query.Enhancer) Option {
	return func(k *Knowledge) {
		k.queryEnhancer = qe
	}
}

// WithReranker sets the reranker for the Knowledge instance.
func WithReranker(rr reranker.Reranker) Option {
	return func(k *Knowledge) {
		k.reranker = rr
	}
}

// WithSources sets the sources for the Knowledge instance.
func WithSources(sources []source.Source) Option {
	return func(k *Knowledge) {
		k.sources = sources
	}
}

// WithImportDocumentHook adds a middleware hook for document import.
// Multiple hooks can be added and will be executed in the order they are added.
// Each hook wraps the next function in the chain.
//
// Example:
//
//	hook := func(next ImportDocumentFunc) ImportDocumentFunc {
//	    return func(ctx context.Context, src source.Source, doc *document.Document) (*ImportResult, error) {
//	        // Before import logic
//	        log.Infof("Importing document: %s", doc.ID)
//
//	        // Call next in chain
//	        result, err := next(ctx, src, doc)
//
//	        // After import logic with result
//	        if err == nil {
//	            log.Infof("Successfully imported: %s, task_code=%s, documents=%d",
//	                doc.ID, result.TaskCode, result.DocumentNum)
//	        }
//	        return result, err
//	    }
//	}
func WithImportDocumentHook(hook ImportDocumentHook) Option {
	return func(k *Knowledge) {
		k.importDocumentHooks = append(k.importDocumentHooks, hook)
	}
}

// WithDisableRemoteChunking disables TRag's remote chunking and uses local chunking instead.
// When enabled, documents will be sent directly to TRag using ImportDocument interface
// instead of ImportFile interface, allowing users to have full control over chunking.
//
// Default: false (use TRag's remote chunking)
//
// Note: When using local chunking, you should prepare properly chunked content
// before passing it to the knowledge base. TRag will not perform additional chunking.
func WithDisableRemoteChunking(disable bool) Option {
	return func(k *Knowledge) {
		k.disableRemoteChunking = disable
	}
}
