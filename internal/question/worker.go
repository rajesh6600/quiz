package question

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

/*
Pseudocode: question_fetcher worker
-----------------------------------
while true:
    req = dequeue_request()
    ctx = context_with_timeout(QUESTION_FETCH_TIMEOUT_SECONDS)
    pack, err = questionService.FetchPack(ctx, req)
    if err == nil:
        cache.Store(req, pack)
        continue
    if isInsufficientError(err):
        aiGenerator.EnqueuePack(ctx, {
            category: req.Category,
            difficulty: pickMissingDifficulty(err),
            count: err.MissingCount,
            seed: req.Seed,
        })
    log_error(err)
*/

// FetcherWorker proactively fetches/caches packs to keep latency low.
type FetcherWorker struct {
	service   *Service
	ai        AIGenerator
	queue     <-chan PackRequest
	logger    zerolog.Logger
	timeout   time.Duration
	shutdownC chan struct{}
}

func NewFetcherWorker(service *Service, ai AIGenerator, queue <-chan PackRequest, logger zerolog.Logger, timeout time.Duration) *FetcherWorker {
	if timeout <= 0 {
		timeout = 4 * time.Second
	}
	return &FetcherWorker{
		service:   service,
		ai:        ai,
		queue:     queue,
		logger:    logger,
		timeout:   timeout,
		shutdownC: make(chan struct{}),
	}
}

func (w *FetcherWorker) Run() {
	for {
		select {
		case <-w.shutdownC:
			w.logger.Info().Msg("question fetcher stopping")
			return
		case req := <-w.queue:
			w.handle(req)
		}
	}
}

func (w *FetcherWorker) handle(req PackRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), w.timeout)
	defer cancel()

	if _, err := w.service.FetchPack(ctx, req); err != nil {
		w.logger.Warn().Err(err).Msg("prefetch failed")
		if w.ai != nil {
			// When AI fallback is required asynchronously, enqueue so the generator can pre-warm packs.
			if enqueueErr := w.ai.EnqueuePack(ctx, AIGenerateRequest{
				Category: req.Category,
				// simple heuristic: request more medium questions if unspecified
				Difficulty: pickFirstDifficulty(req.DifficultyCounts),
				Count:      req.TotalQuestions,
				Seed:       req.Seed,
			}); enqueueErr != nil {
				w.logger.Error().Err(enqueueErr).Msg("ai enqueue failed")
			}
		}
	}
}

func (w *FetcherWorker) Stop() {
	close(w.shutdownC)
}

func pickFirstDifficulty(m map[string]int) string {
	for diff := range m {
		return diff
	}
	return DifficultyEasy
}
