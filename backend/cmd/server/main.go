package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/luxarts/supervaisor/internal/agent"
	"github.com/luxarts/supervaisor/internal/api"
	"github.com/luxarts/supervaisor/internal/hooks"
	"github.com/luxarts/supervaisor/internal/state"
	"github.com/luxarts/supervaisor/internal/ws"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Shared dependencies
	store := state.NewStore()
	hub := ws.NewHub()
	manager := agent.NewManager()

	// Handlers
	apiHandler := api.NewHandler(store, manager, hub, port)
	hookHandler := hooks.NewHandler(store, hub)

	r := gin.Default()

	// CORS for local frontend dev (Vite default is :5173)
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Routes
	r.GET("/agents", apiHandler.ListAgents)
	r.POST("/agents", apiHandler.CreateAgent)
	r.DELETE("/agents/:id", apiHandler.DeleteAgent)
	r.POST("/hooks", hookHandler.Handle)
	r.GET("/ws", apiHandler.WebSocket)

	log.Printf("supervAIsor backend listening on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
