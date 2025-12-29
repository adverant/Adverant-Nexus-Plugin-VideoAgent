package similarity

import (
	"context"
	"fmt"
	"log"

	"github.com/adverant/nexus/videoagent-worker/internal/clients"
)

// SimilarityModule encapsulates all similarity search components
type SimilarityModule struct {
	VideoEmbedder *VideoEmbedder
	SceneEmbedder *SceneEmbedder
	SearchAPI     *SearchAPI
	QdrantManager *QdrantManager
}

// InitializeSimilarityModule initializes all similarity search components
// This is the single entry point for setting up the complete similarity system
func InitializeSimilarityModule(
	mageAgent *clients.MageAgentClient,
	graphragClient *clients.GraphRAGClient,
	qdrantEndpoint string,
	qdrantAPIKey string,
) (*SimilarityModule, error) {
	log.Println("Initializing similarity module...")

	// 1. Initialize Qdrant manager
	qdrantManager := NewQdrantManager(qdrantEndpoint, qdrantAPIKey)
	log.Printf("✓ Qdrant manager initialized (endpoint: %s)", qdrantEndpoint)

	// 2. Initialize video embedder with GraphRAG client
	videoEmbedder := NewVideoEmbedder(mageAgent, graphragClient)
	log.Println("✓ Video embedder initialized (1024-D VoyageAI voyage-3)")

	// 3. Initialize scene embedder
	sceneEmbedder := NewSceneEmbedder(videoEmbedder)
	log.Println("✓ Scene embedder initialized")

	// 4. Initialize search API
	searchAPI := NewSearchAPI(videoEmbedder, sceneEmbedder, qdrantManager, graphragClient)
	log.Println("✓ Search API initialized")

	module := &SimilarityModule{
		VideoEmbedder: videoEmbedder,
		SceneEmbedder: sceneEmbedder,
		SearchAPI:     searchAPI,
		QdrantManager: qdrantManager,
	}

	log.Println("✓ Similarity module initialization complete")
	return module, nil
}

// InitializeCollections initializes Qdrant collections (1024-D vectors)
// Call this after initializing the module to ensure collections exist
func (sm *SimilarityModule) InitializeCollections(ctx context.Context) error {
	log.Println("Initializing Qdrant collections (1024-D)...")

	if err := sm.QdrantManager.InitializeCollections(ctx); err != nil {
		return fmt.Errorf("failed to initialize Qdrant collections: %w", err)
	}

	log.Println("✓ Qdrant collections initialized:")
	log.Printf("  - video_embeddings: 1024-D, Cosine distance")
	log.Printf("  - scene_embeddings: 1024-D, Cosine distance")

	return nil
}

// HealthCheck verifies all components are operational
func (sm *SimilarityModule) HealthCheck(ctx context.Context) error {
	// TODO: Implement health checks for each component
	// - Qdrant connectivity
	// - GraphRAG endpoint availability
	// - MageAgent endpoint availability
	return nil
}
