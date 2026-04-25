package streams

import (
	"context"
	"sync"
	"time"
)

type StreamBufferingConfig struct {
	Enabled        bool
	ChunkThreshold int
	TimerDuration  time.Duration
}

func DefaultStreamBufferingConfig() StreamBufferingConfig {
	return StreamBufferingConfig{
		Enabled:        true,
		ChunkThreshold: 16,
		TimerDuration:  3 * time.Second,
	}
}

type bufferedStream struct {
	source    Stream[any]
	config    StreamBufferingConfig
	ttftReady func() bool

	mu         sync.Mutex
	buffer     []any
	flushed    bool
	firstChunk bool

	output chan any
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	current any
}

func NewBufferedStream(source Stream[any], config StreamBufferingConfig, ttftReady func() bool) Stream[any] {
	if !config.Enabled {
		return source
	}
	if config.ChunkThreshold <= 0 {
		config.ChunkThreshold = 16
	}
	if config.TimerDuration <= 0 {
		config.TimerDuration = 3 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	bs := &bufferedStream{
		source:     source,
		config:     config,
		ttftReady:  ttftReady,
		buffer:     make([]any, 0, config.ChunkThreshold),
		flushed:    false,
		firstChunk: true,
		output:     make(chan any, 100),
		ctx:        ctx,
		cancel:     cancel,
	}

	bs.wg.Add(1)
	go bs.run()
	return bs
}

type sourceResult struct {
	chunk any
	ok    bool
}

func (s *bufferedStream) readSourceOrTimer(timerCh <-chan time.Time) (any, bool, bool) {
	resultCh := make(chan sourceResult, 1)
	go func() {
		ok := s.source.Next()
		var chunk any
		if ok {
			chunk = s.source.Current()
		}
		resultCh <- sourceResult{chunk: chunk, ok: ok}
	}()

	select {
	case result := <-resultCh:
		return result.chunk, result.ok, false
	case <-timerCh:
		s.mu.Lock()
		s.flushed = true
		chunks := s.buffer
		s.buffer = nil
		s.mu.Unlock()
		s.sendChunks(chunks)
		result := <-resultCh
		return result.chunk, result.ok, true
	case <-s.ctx.Done():
		return nil, false, false
	}
}

func (s *bufferedStream) sendChunk(chunk any) {
	select {
	case s.output <- chunk:
	case <-s.ctx.Done():
	}
}

func (s *bufferedStream) sendChunks(chunks []any) {
	for _, c := range chunks {
		select {
		case s.output <- c:
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *bufferedStream) run() {
	defer func() {
		close(s.output)
		s.wg.Done()
	}()

	var timer *time.Timer
	var timerActive bool
	var timerCh <-chan time.Time

	for {
		var chunk any
		var ok bool

		if timerActive {
			var timerFired bool
			chunk, ok, timerFired = s.readSourceOrTimer(timerCh)
			if timerFired {
				timerActive = false
				timerCh = nil
				if timer != nil {
					timer.Stop()
				}
			}
		} else {
			ok = s.source.Next()
			if ok {
				chunk = s.source.Current()
			}
		}

		if !ok {
			s.mu.Lock()
			if len(s.buffer) > 0 {
				s.flushed = true
				if timerActive {
					timer.Stop()
				}
				chunks := s.buffer
				s.buffer = nil
				s.mu.Unlock()
				s.sendChunks(chunks)
			} else {
				s.mu.Unlock()
			}
			return
		}

		s.mu.Lock()
		if s.flushed {
			s.mu.Unlock()
			s.sendChunk(chunk)
			continue
		}

		s.buffer = append(s.buffer, chunk)

		if s.firstChunk && s.ttftReady() {
			s.firstChunk = false
			timer = time.NewTimer(s.config.TimerDuration)
			timerActive = true
			timerCh = timer.C
		}

		if len(s.buffer) >= s.config.ChunkThreshold {
			s.flushed = true
			if timerActive {
				timer.Stop()
				timerActive = false
				timerCh = nil
			}
			chunks := s.buffer
			s.buffer = nil
			s.mu.Unlock()
			s.sendChunks(chunks)
			continue
		}
		s.mu.Unlock()
	}
}

func (s *bufferedStream) Next() bool {
	select {
	case chunk, ok := <-s.output:
		if !ok {
			s.mu.Lock()
			s.current = nil
			s.mu.Unlock()
			return false
		}
		s.mu.Lock()
		s.current = chunk
		s.mu.Unlock()
		return true
	case <-s.ctx.Done():
		s.mu.Lock()
		s.current = nil
		s.mu.Unlock()
		return false
	}
}

func (s *bufferedStream) Current() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *bufferedStream) Err() error {
	return s.source.Err()
}

func (s *bufferedStream) Close() error {
	err := s.source.Close()
	s.cancel()
	s.wg.Wait()
	return err
}