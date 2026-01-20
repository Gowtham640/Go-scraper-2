package main

import (
	"fmt"
	"net/http"
	"srm-academia-scraper/config"
	"srm-academia-scraper/handlers"
	"srm-academia-scraper/jobs"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/middleware"
	"srm-academia-scraper/storage"
	"srm-academia-scraper/worker"
	"time"

	"golang.org/x/time/rate"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Fatal("config_load", "Failed to load configuration", err)
	}

	logger.Info("server_start", "Starting SRM Academia Scraper", map[string]interface{}{
		"port": cfg.Port,
	})

	// Initialize Supabase client
	db, err := storage.NewSupabaseClient(cfg.SupabaseURL, cfg.EncryptionKey)
	if err != nil {
		logger.Fatal("supabase_init", "Failed to initialize Supabase client", err)
	}

	logger.Info("supabase_init", "Supabase client initialized", nil)

	// Start background worker
	jobWorker := worker.NewWorker(db)
	jobWorker.Start()
	logger.Info("worker_init", "Background worker started", nil)

	// Create job manager
	jobManager := jobs.NewManager(db, jobWorker)
	logger.Info("job_manager_init", "Job manager initialized", nil)

	// Create rate limiter (1 request per second per IP)
	rateLimiter := middleware.NewRateLimiter(rate.Limit(1), 3)
	rateLimiter.CleanupOldLimiters(10 * time.Minute)

	// Create router
	mux := http.NewServeMux()

	// Register routes
	mux.HandleFunc("/login", handlers.LoginHandler(db))
	mux.HandleFunc("/user", handlers.UserHandler(jobManager))
	mux.HandleFunc("/courses", handlers.CoursesHandler(jobManager))
	mux.HandleFunc("/timetable", handlers.TimetableHandler(jobManager))
	mux.HandleFunc("/calendar", handlers.CalendarHandler(jobManager))
	mux.HandleFunc("/health", handlers.HealthHandler())

	// Apply middleware
	handler := middleware.Logging(
		middleware.CORS(
			rateLimiter.Middleware(mux),
		),
	)

	// Start server
	addr := fmt.Sprintf(":%s", cfg.Port)
	logger.Info("server_start", fmt.Sprintf("Server listening on port %s", cfg.Port), map[string]interface{}{
		"address": addr,
	})

	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil {
		logger.Fatal("server_start", "Failed to start server", err)
	}
}
