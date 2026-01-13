package main

import (
	"net/http"
)

type Server struct {
	config         Config
	mux            *http.ServeMux
	proxy          *Proxy
	logger         *Logger
	sessionManager *SessionManager
}

func NewServer(cfg Config) (*Server, error) {
	logger, err := NewLogger(cfg.LogDir)
	if err != nil {
		return nil, err
	}

	sessionManager, err := NewSessionManager(cfg.LogDir, logger)
	if err != nil {
		logger.Close()
		return nil, err
	}

	s := &Server{
		config:         cfg,
		mux:            http.NewServeMux(),
		proxy:          NewProxyWithSessionManager(logger, sessionManager),
		logger:         logger,
		sessionManager: sessionManager,
	}
	s.mux.HandleFunc("/health", s.handleHealth)
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check if it's a known endpoint
	if r.URL.Path == "/health" {
		s.handleHealth(w, r)
		return
	}

	// Otherwise, proxy the request
	s.proxy.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (s *Server) Close() error {
	var err error
	if s.sessionManager != nil {
		err = s.sessionManager.Close()
	}
	if s.logger != nil {
		if logErr := s.logger.Close(); logErr != nil && err == nil {
			err = logErr
		}
	}
	return err
}
