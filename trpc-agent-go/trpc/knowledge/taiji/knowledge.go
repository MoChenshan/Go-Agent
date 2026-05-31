// Package taiji provides the knowledge base that uses Taiji for semantic search.
package taiji

import (
	"context"
	"fmt"
	"sync"
	"time"

	ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"
	client "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/taiji"
	taijiretriever "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/taiji/retriever"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/taiji/sdk"
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

// Option is an option for the Knowledge instance.
type Option func(*Knowledge)

// Knowledge is a knowledge base that uses Taiji for semantic search.
type Knowledge struct {
	taijiOption sdk.TaijiOption
	client      *client.Client

	embedder      embedder.Embedder
	retriever     retriever.Retriever
	queryEnhancer query.Enhancer
	reranker      reranker.Reranker
	sources       []source.Source
	rateLimiter   *rate.Limiter
}

// New creates a new Knowledge instance.
func New(opts ...Option) (*Knowledge, error) {
	k := &Knowledge{}

	for _, opt := range opts {
		opt(k)
	}

	if err := sdk.CheckTaijiOption(&k.taijiOption); err != nil {
		return nil, err
	}

	var httpClient ihttp.HTTPClient
	serviceName := k.taijiOption.ServiceName
	if k.taijiOption.ClientBuilder != nil {
		httpClient = k.taijiOption.ClientBuilder(sdk.WithHTTPClientName(serviceName))
	}

	internalTaijiOption := client.TaijiOption{
		URL:             k.taijiOption.URL,
		Token:           k.taijiOption.Token,
		ServiceName:     k.taijiOption.ServiceName,
		TaijiHYAPIURL:   k.taijiOption.TaijiHYAPIURL,
		TaijiHYAPIToken: k.taijiOption.TaijiHYAPIToken,
		KnowledgeOption: client.KnowledgeOption{
			EmbIndex: k.taijiOption.EmbIndex,
			WSID:     k.taijiOption.WSID,
		},
	}

	// Create Taiji client
	k.client = client.NewClient(client.WithTaijiOption(internalTaijiOption), client.WithHTTPClient(httpClient))
	if k.retriever == nil {
		if k.reranker == nil {
			k.reranker = topk.New()
		}
		if k.queryEnhancer == nil {
			k.queryEnhancer = query.NewPassthroughEnhancer()
		}
		retriever, err := taijiretriever.New(
			taijiretriever.WithTaijiOption(k.taijiOption),
			taijiretriever.WithReRanker(k.reranker),
			taijiretriever.WithQueryEnhancer(k.queryEnhancer),
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

// WithTaijiRateLimit sets the rate limit for Taiji UpdateIndexData calls.
// interval: minimum interval between requests (0 = no limit)
// burst: maximum burst size (0 = use default)
func WithTaijiRateLimit(interval time.Duration, burst int) LoadOption {
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

// Load loads the knowledge source and returns the list of inserted document IDs
func (dk *Knowledge) Load(ctx context.Context, opts ...LoadOption) ([]string, error) {
	config := &loadOptions{}
	for _, opt := range opts {
		opt(config)
	}

	// Set up rate limiter for UpdateIndexData calls if specified
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
		return nil, fmt.Errorf("srcParallelism cannot be negative: %d", config.srcParallelism)
	}

	if config.docParallelism == 0 {
		// Default to serial processing
		config.docParallelism = 1
	} else if config.docParallelism < 0 {
		return nil, fmt.Errorf("docParallelism cannot be negative: %d", config.docParallelism)
	}

	// Timing variables.
	startTime := time.Now()
	totalSources := len(dk.sources)
	log.Infof("Starting knowledge base loading with %d sources", totalSources)

	// Use the concurrent loader when there is any real parallelism to gain.
	var err error
	var documentIDs []string
	if config.srcParallelism > 1 || config.docParallelism > 1 {
		documentIDs, err = dk.loadConcurrent(ctx, config)
	} else {
		for i := range dk.sources {
			var srcDocIDs []string
			srcDocIDs, err = dk.loadSingleSrc(ctx, i, nil)
			if err != nil {
				break
			}
			documentIDs = append(documentIDs, srcDocIDs...)
		}
	}

	if err != nil {
		return nil, err
	}

	elapsedTotal := time.Since(startTime)
	log.Infof("Knowledge base loading completed in %s (%d sources, %d documents)",
		elapsedTotal, totalSources, len(documentIDs))
	return documentIDs, nil
}

func (dk *Knowledge) loadConcurrent(ctx context.Context, config *loadOptions) ([]string, error) {
	srcParallelism := max(config.srcParallelism, 1)
	docParallelism := max(config.docParallelism, 1)
	srcQueue := make(chan struct{}, srcParallelism)
	docQueue := make(chan struct{}, docParallelism)

	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error
	var mu sync.Mutex
	var allDocumentIDs []string

	for i := range dk.sources {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			srcQueue <- struct{}{}
			defer func() {
				<-srcQueue
			}()

			docIDs, err := dk.loadSingleSrc(ctx, index, docQueue)
			if err != nil {
				log.Errorf("Failed to load source %s: %v", dk.sources[index].Name(), err)
				errOnce.Do(func() {
					firstErr = err
				})
				return
			}

			mu.Lock()
			allDocumentIDs = append(allDocumentIDs, docIDs...)
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return allDocumentIDs, nil
}

func (dk *Knowledge) loadSingleSrc(ctx context.Context, index int, docQueue chan struct{}) ([]string, error) {
	src := dk.sources[index]
	sourceName := src.Name()
	sourceType := src.Type()
	log.Infof("Loading source %d/%d: %s (type: %s)", index+1, len(dk.sources), sourceName, sourceType)

	docs, err := src.ReadDocuments(ctx)
	if err != nil {
		log.Errorf("Failed to read documents from source %s: %v", sourceName, err)
		return nil, fmt.Errorf("failed to read documents from source %s: %w", sourceName, err)
	}
	log.Infof("Fetched %d document(s) from source %s", len(docs), sourceName)
	log.Infof("Start embedding & storing documents from source %s...", sourceName)

	var documentIDs []string
	if docQueue == nil {
		// load docs sequentially
		for j, doc := range docs {
			now := time.Now()
			if err := dk.addDocument(ctx, src, doc); err != nil {
				log.Errorf("Failed to add document from source %s: %v", sourceName, err)
				return nil, fmt.Errorf("failed to add document from source %s: %w", sourceName, err)
			}
			cost := time.Since(now)
			documentIDs = append(documentIDs, doc.ID)
			log.Infof("Progress: %d/%d (%.2f%%) documents from source %s, cost: %s",
				j+1, len(docs), float64(j+1)/float64(len(docs))*100, sourceName, cost)
		}
	} else {
		// load docs concurrently
		var wg sync.WaitGroup
		var errOnce sync.Once
		var firstErr error
		var mu sync.Mutex

		for j, doc := range docs {
			wg.Add(1)
			go func(index int, doc *document.Document) {
				defer wg.Done()

				docQueue <- struct{}{}
				defer func() {
					<-docQueue
				}()

				now := time.Now()
				if err := dk.addDocument(ctx, src, doc); err != nil {
					log.Errorf("Failed to add document from source %s: %v", sourceName, err)
					errOnce.Do(func() {
						firstErr = fmt.Errorf("failed to add document from source %s: %w", sourceName, err)
					})
					return
				}

				mu.Lock()
				documentIDs = append(documentIDs, doc.ID)
				mu.Unlock()

				cost := time.Since(now)
				log.Infof("Progress: %d/%d (%.2f%%) documents from source %s processed, cost: %s",
					index+1, len(docs), float64(index+1)/float64(len(docs))*100, sourceName, cost)
			}(j, doc)
		}
		wg.Wait()

		if firstErr != nil {
			return nil, firstErr
		}
	}

	log.Infof("Successfully loaded source %s", sourceName)
	return documentIDs, nil
}

// addDocument adds a document to the knowledge base using Taiji's UpdateIndexData API.
func (dk *Knowledge) addDocument(ctx context.Context, src source.Source, doc *document.Document) error {
	// Use rate limiter if configured
	if dk.rateLimiter != nil {
		if err := dk.rateLimiter.Wait(ctx); err != nil {
			return err
		}
	}

	// Create index data request
	req := &client.IndexDataRequest{
		EmbIndexID: src.Name(), // Use source name as index ID
		Command:    client.CommandAdd,
		Data: []client.IndexDataItem{
			{
				ID:    doc.ID,
				Value: doc.Content,
				Query: doc.Content, // Use content as query for embedding
			},
		},
	}

	resp, err := dk.client.UpdateIndexData(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to update index data: %w", err)
	}

	log.Infof("Taiji add document success: %s", resp.Msg)
	return nil
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

	// Use built-in retriever for RAG pipeline.
	retrieverReq := &retriever.Query{
		Text:     req.Query,
		Limit:    req.MaxResults,
		MinScore: minScore,
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

// convertConversationHistory converts conversation messages to query format.
func convertConversationHistory(history []knowledge.ConversationMessage) []query.ConversationMessage {
	result := make([]query.ConversationMessage, len(history))
	for i, msg := range history {
		result[i] = query.ConversationMessage(msg)
	}
	return result
}

// WithTaijiOption set the taiji client options
func WithTaijiOption(opt sdk.TaijiOption) Option {
	return func(k *Knowledge) {
		k.taijiOption = opt
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
