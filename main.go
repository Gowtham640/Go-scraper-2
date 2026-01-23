package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"srm-academia-scraper/config"
	"srm-academia-scraper/handlers"
	"srm-academia-scraper/jobs"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/middleware"
	"srm-academia-scraper/storage"
	"srm-academia-scraper/worker"
	"sync"
	"syscall"
	"time"

	"github.com/playwright-community/playwright-go"
	"golang.org/x/time/rate"
)

// Global browser instance and semaphore
var (
	globalBrowser     playwright.Browser
	globalBrowserOnce sync.Once
	loginSemaphore    = make(chan struct{}, 3) // Capacity 3 for concurrent contexts
)

// getGlobalBrowser returns the singleton browser instance, launching it if needed
func getGlobalBrowser() playwright.Browser {
	globalBrowserOnce.Do(func() {
		pw, err := playwright.Run()
		if err != nil {
			logger.Fatal("browser_init", "Failed to start Playwright", err)
		}

		browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
			Headless: playwright.Bool(false),
			Args: []string{
				"--no-sandbox",
				"--disable-setuid-sandbox",
				"--disable-dev-shm-usage",
				"--disable-accelerated-2d-canvas",
				"--no-first-run",
				"--no-zygote",
				"--disable-gpu",
			},
		})
		if err != nil {
			logger.Fatal("browser_init", "Failed to launch browser", err)
		}

		globalBrowser = browser
		logger.Info("browser_init", "Global browser instance launched", nil)
	})
	return globalBrowser
}

// shutdownGlobalBrowser closes the global browser instance
func shutdownGlobalBrowser() {
	if globalBrowser != nil {
		if err := globalBrowser.Close(); err != nil {
			logger.Error("browser_shutdown", "Failed to close global browser", err, nil)
		} else {
			logger.Info("browser_shutdown", "Global browser instance closed", nil)
		}
		globalBrowser = nil
	}
}

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
	mux.HandleFunc("/attendance", handlers.AttendanceHandler(jobManager))
	mux.HandleFunc("/marks", handlers.MarksHandler(jobManager))
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

	// Graceful shutdown on interrupt/terminate signals
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-shutdownCh
		logger.Info("server_shutdown", fmt.Sprintf("Signal %s received, shutting down", sig), nil)
		shutdownGlobalBrowser()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("server_shutdown", "Graceful shutdown failed", err, nil)
		}
	}()

	if err := server.ListenAndServe(); err != nil {
		logger.Fatal("server_start", "Failed to start server", err)
	}
}
