package cache

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type fakeRedisBackend struct {
	mu      sync.Mutex
	store   map[string]fakeRedisValue
	conns   map[net.Conn]struct{}
	closed  chan struct{}
	clients sync.WaitGroup
}

type fakeRedisValue struct {
	value     []byte
	expiresAt time.Time
}

func newFakeRedisBackend() *fakeRedisBackend {
	return &fakeRedisBackend{
		store:  make(map[string]fakeRedisValue),
		conns:  make(map[net.Conn]struct{}),
		closed: make(chan struct{}),
	}
}

func (s *fakeRedisBackend) Dialer(_ context.Context, _, _ string) (net.Conn, error) {
	select {
	case <-s.closed:
		return nil, errors.New("fake redis closed")
	default:
	}

	clientConn, serverConn := net.Pipe()
	s.mu.Lock()
	s.conns[serverConn] = struct{}{}
	s.mu.Unlock()

	s.clients.Add(1)
	go func() {
		defer s.clients.Done()
		defer func() {
			s.mu.Lock()
			delete(s.conns, serverConn)
			s.mu.Unlock()
		}()
		defer serverConn.Close()
		s.handleConnection(serverConn)
	}()

	return clientConn, nil
}

func (s *fakeRedisBackend) Close() {
	select {
	case <-s.closed:
		return
	default:
		close(s.closed)
	}

	s.mu.Lock()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()

	s.clients.Wait()
}

func (s *fakeRedisBackend) handleConnection(conn net.Conn) {
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	for {
		args, err := readCommand(r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			_ = writeError(w, "ERR protocol error")
			_ = w.Flush()
			return
		}

		if len(args) == 0 {
			_ = writeError(w, "ERR empty command")
			_ = w.Flush()
			continue
		}

		if err := s.execute(args, w); err != nil {
			_ = writeError(w, err.Error())
		}
		if err := w.Flush(); err != nil {
			return
		}
	}
}

func readCommand(r *bufio.Reader) ([]string, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	if prefix != '*' {
		return nil, fmt.Errorf("expected array")
	}

	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		return nil, err
	}

	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		p, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if p != '$' {
			return nil, fmt.Errorf("expected bulk string")
		}

		lenLine, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		l, err := strconv.Atoi(strings.TrimSpace(lenLine))
		if err != nil {
			return nil, err
		}
		if l < 0 {
			args = append(args, "")
			continue
		}

		buf := make([]byte, l+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:l]))
	}

	return args, nil
}

func (s *fakeRedisBackend) execute(args []string, w *bufio.Writer) error {
	cmd := strings.ToUpper(args[0])
	s.cleanupExpired()

	switch cmd {
	case "PING":
		return writeSimpleString(w, "PONG")

	case "SET":
		if len(args) < 3 {
			return writeError(w, "ERR wrong number of arguments for 'set'")
		}
		key := args[1]
		val := []byte(args[2])
		var (
			expiresAt time.Time
			nx        bool
		)

		for i := 3; i < len(args); i++ {
			tok := strings.ToUpper(args[i])
			switch tok {
			case "EX":
				if i+1 >= len(args) {
					return writeError(w, "ERR syntax error")
				}
				sec, err := strconv.Atoi(args[i+1])
				if err != nil {
					return writeError(w, "ERR invalid expire")
				}
				expiresAt = time.Now().Add(time.Duration(sec) * time.Second)
				i++
			case "PX":
				if i+1 >= len(args) {
					return writeError(w, "ERR syntax error")
				}
				ms, err := strconv.Atoi(args[i+1])
				if err != nil {
					return writeError(w, "ERR invalid expire")
				}
				expiresAt = time.Now().Add(time.Duration(ms) * time.Millisecond)
				i++
			case "NX":
				nx = true
			}
		}

		s.mu.Lock()
		if nx {
			if _, ok := s.getValueLocked(key); ok {
				s.mu.Unlock()
				return writeNull(w)
			}
		}
		s.store[key] = fakeRedisValue{value: val, expiresAt: expiresAt}
		s.mu.Unlock()
		return writeSimpleString(w, "OK")

	case "GET":
		if len(args) != 2 {
			return writeError(w, "ERR wrong number of arguments for 'get'")
		}
		s.mu.Lock()
		v, ok := s.getValueLocked(args[1])
		s.mu.Unlock()
		if !ok {
			return writeNull(w)
		}
		return writeBulkString(w, string(v))

	case "EXISTS":
		if len(args) < 2 {
			return writeInteger(w, 0)
		}
		count := int64(0)
		s.mu.Lock()
		for _, key := range args[1:] {
			if _, ok := s.getValueLocked(key); ok {
				count++
			}
		}
		s.mu.Unlock()
		return writeInteger(w, count)

	case "DEL":
		if len(args) < 2 {
			return writeInteger(w, 0)
		}
		deleted := int64(0)
		s.mu.Lock()
		for _, key := range args[1:] {
			if _, ok := s.getValueLocked(key); ok {
				delete(s.store, key)
				deleted++
			}
		}
		s.mu.Unlock()
		return writeInteger(w, deleted)

	case "INCR":
		if len(args) != 2 {
			return writeError(w, "ERR wrong number of arguments for 'incr'")
		}
		key := args[1]
		s.mu.Lock()
		v, ok := s.getValueLocked(key)
		current := int64(0)
		if ok {
			parsed, err := strconv.ParseInt(string(v), 10, 64)
			if err != nil {
				s.mu.Unlock()
				return writeError(w, "ERR value is not an integer")
			}
			current = parsed
		}
		current++
		s.store[key] = fakeRedisValue{value: []byte(strconv.FormatInt(current, 10))}
		s.mu.Unlock()
		return writeInteger(w, current)

	case "EXPIRE":
		if len(args) != 3 {
			return writeError(w, "ERR wrong number of arguments for 'expire'")
		}
		seconds, err := strconv.Atoi(args[2])
		if err != nil {
			return writeError(w, "ERR value is not an integer")
		}
		s.mu.Lock()
		entry, ok := s.getEntryLocked(args[1])
		if !ok {
			s.mu.Unlock()
			return writeInteger(w, 0)
		}
		entry.expiresAt = time.Now().Add(time.Duration(seconds) * time.Second)
		s.store[args[1]] = entry
		s.mu.Unlock()
		return writeInteger(w, 1)

	case "TTL":
		if len(args) != 2 {
			return writeError(w, "ERR wrong number of arguments for 'ttl'")
		}
		s.mu.Lock()
		entry, ok := s.getEntryLocked(args[1])
		s.mu.Unlock()
		if !ok {
			return writeInteger(w, -2)
		}
		if entry.expiresAt.IsZero() {
			return writeInteger(w, -1)
		}
		remaining := time.Until(entry.expiresAt)
		if remaining < 0 {
			return writeInteger(w, -2)
		}
		return writeInteger(w, int64(remaining.Seconds()))

	case "EVALSHA":
		return writeError(w, "NOSCRIPT No matching script. Please use EVAL.")

	case "EVAL":
		if len(args) < 5 {
			return writeError(w, "ERR wrong number of arguments for 'eval'")
		}
		numKeys, err := strconv.Atoi(args[2])
		if err != nil || numKeys != 1 {
			return writeError(w, "ERR only one key is supported")
		}
		key := args[3]
		expSeconds, err := strconv.Atoi(args[4])
		if err != nil {
			return writeError(w, "ERR invalid expiration")
		}

		s.mu.Lock()
		entry, exists := s.getEntryLocked(key)
		v := entry.value
		count := int64(0)
		if exists {
			parsed, parseErr := strconv.ParseInt(string(v), 10, 64)
			if parseErr == nil {
				count = parsed
			}
		}
		count++
		entry = fakeRedisValue{value: []byte(strconv.FormatInt(count, 10)), expiresAt: entry.expiresAt}
		if count == 1 {
			entry.expiresAt = time.Now().Add(time.Duration(expSeconds) * time.Second)
		}
		s.store[key] = entry
		s.mu.Unlock()
		return writeInteger(w, count)

	case "SCAN":
		// Minimal SCAN implementation: returns all matching keys in one response.
		// Args: SCAN cursor [MATCH pattern] [COUNT count]
		pattern := "*"
		for i := 2; i < len(args); i++ {
			if strings.ToUpper(args[i]) == "MATCH" && i+1 < len(args) {
				pattern = args[i+1]
				i++
			}
		}

		s.mu.Lock()
		var matched []string
		for k := range s.store {
			if matchGlob(pattern, k) {
				if entry, ok := s.getEntryLocked(k); ok {
					_ = entry
					matched = append(matched, k)
				}
			}
		}
		s.mu.Unlock()

		// Return: array of [cursor, [keys...]]
		// cursor "0" means scan is complete
		_, _ = w.WriteString("*2\r\n")
		_ = writeBulkString(w, "0")
		_, _ = w.WriteString("*" + strconv.Itoa(len(matched)) + "\r\n")
		for _, k := range matched {
			_ = writeBulkString(w, k)
		}
		return nil

	default:
		return writeError(w, "ERR unknown command")
	}
}

func (s *fakeRedisBackend) cleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.store {
		if !v.expiresAt.IsZero() && now.After(v.expiresAt) {
			delete(s.store, k)
		}
	}
}

func (s *fakeRedisBackend) getValueLocked(key string) ([]byte, bool) {
	entry, ok := s.getEntryLocked(key)
	if !ok {
		return nil, false
	}
	return entry.value, true
}

func (s *fakeRedisBackend) getEntryLocked(key string) (fakeRedisValue, bool) {
	entry, ok := s.store[key]
	if !ok {
		return fakeRedisValue{}, false
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(s.store, key)
		return fakeRedisValue{}, false
	}
	return entry, true
}

func writeSimpleString(w *bufio.Writer, value string) error {
	_, err := w.WriteString("+" + value + "\r\n")
	return err
}

func writeError(w *bufio.Writer, value string) error {
	_, err := w.WriteString("-" + value + "\r\n")
	return err
}

func writeInteger(w *bufio.Writer, value int64) error {
	_, err := w.WriteString(":" + strconv.FormatInt(value, 10) + "\r\n")
	return err
}

func writeBulkString(w *bufio.Writer, value string) error {
	_, err := w.WriteString("$" + strconv.Itoa(len(value)) + "\r\n" + value + "\r\n")
	return err
}

func writeNull(w *bufio.Writer) error {
	_, err := w.WriteString("$-1\r\n")
	return err
}

// matchGlob performs simple glob matching (only supports * and ?).
func matchGlob(pattern, s string) bool {
	if pattern == "*" {
		return true
	}
	return globMatch(pattern, s)
}

func globMatch(pattern, s string) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			// Try matching rest of pattern against every suffix of s
			pattern = pattern[1:]
			if len(pattern) == 0 {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if globMatch(pattern, s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(s) == 0 {
				return false
			}
			pattern = pattern[1:]
			s = s[1:]
		default:
			if len(s) == 0 || s[0] != pattern[0] {
				return false
			}
			pattern = pattern[1:]
			s = s[1:]
		}
	}
	return len(s) == 0
}
