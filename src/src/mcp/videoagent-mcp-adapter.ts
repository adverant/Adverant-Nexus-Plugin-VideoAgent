#!/usr/bin/env node

/**
 * VideoAgent MCP Adapter
 *
 * Enables VideoAgent API to support MCP stdio protocol for Docker Desktop toolkit.
 * Uses the SAME smart-router.ts routing logic as HTTP/WebSocket for consistency.
 *
 * Supported Clients:
 * - Claude Code (via Docker Desktop MCP toolkit)
 * - Claude Desktop (via Docker Desktop MCP toolkit)
 * - Gemini CLI (via MCP stdio protocol)
 * - Codex CLI (via MCP stdio protocol)
 *
 * Architecture:
 * - Claude Code → MCP stdio → THIS ADAPTER → smart-router → VideoAgent Services
 * - HTTP REST → Express routes → smart-router → VideoAgent Services
 * - WebSocket → Socket.IO → smart-router → VideoAgent Services
 *
 * Critical: All clients use identical routing logic, preventing tool failures.
 *
 * Pattern: services/nexus-api-gateway/src/mcp-adapter.ts
 */

import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from '@modelcontextprotocol/sdk/types.js';
import { VIDEOAGENT_TOOLS, TOOL_DESCRIPTIONS } from './videoagent-tools.js';
import { smartRouter, initializeSmartRouter } from './smart-router.js';
import { RedisProducer } from '../queue/RedisProducer.js';
import { MageAgentClient } from '../clients/MageAgentClient.js';
import { VideoAgentWebSocketServer } from '../websocket/VideoAgentWebSocketServer.js';
import { createLogger, format, transports } from 'winston';
import dotenv from 'dotenv';

// Load environment variables
dotenv.config();

// ============================================================================
// VideoAgent MCP Adapter Class
// ============================================================================

class VideoAgentMCPAdapter {
  private server: Server;
  private logger: ReturnType<typeof createLogger>;
  private routerInitialized: boolean = false;

  constructor() {
    // Initialize logger
    this.logger = createLogger({
      level: process.env.LOG_LEVEL || 'info',
      format: format.combine(
        format.timestamp(),
        format.errors({ stack: true }),
        format.json()
      ),
      transports: [
        new transports.Console({
          format: format.combine(
            format.colorize(),
            format.printf(({ timestamp, level, message, ...meta }) => {
              return `${timestamp} [${level}] [VideoAgent MCP]: ${message} ${
                Object.keys(meta).length ? JSON.stringify(meta) : ''
              }`;
            })
          ),
        }),
      ],
    });

    // Initialize MCP server
    this.server = new Server(
      {
        name: 'nexus-videoagent-mcp',
        version: '1.0.0',
      },
      {
        capabilities: {
          tools: {},
        },
      }
    );

    this.setupHandlers();
    this.setupErrorHandlers();

    this.logger.info('VideoAgent MCP Adapter initialized', {
      toolCount: VIDEOAGENT_TOOLS.length,
      tools: Object.keys(TOOL_DESCRIPTIONS),
    });
  }

  /**
   * Initialize smart router with VideoAgent services
   * Must be called before starting the adapter
   */
  async initializeRouter(): Promise<void> {
    if (this.routerInitialized) {
      this.logger.warn('Smart router already initialized');
      return;
    }

    this.logger.info('Initializing smart router...');

    // Get configuration from environment
    const config = {
      redisUrl: process.env.REDIS_URL || 'redis://localhost:6379',
      mageAgentUrl: process.env.MAGEAGENT_URL || 'http://localhost:3000',
      postgresUrl: process.env.POSTGRES_URL || 'postgresql://unified_nexus:graphrag123@localhost:5432/nexus_videoagent',
      qdrantUrl: process.env.QDRANT_URL || 'http://localhost:6333',
      graphragUrl: process.env.GRAPHRAG_URL || 'http://localhost:9090',
    };

    // Initialize services for smart router
    const redisProducer = new RedisProducer(config.redisUrl);
    const mageAgentClient = new MageAgentClient(config.mageAgentUrl);

    // PRODUCTION FIX: WebSocket server not needed for MCP stdio mode
    // MCP uses stdio transport (stdin/stdout), not WebSocket
    // Pass null to smart router to disable WebSocket functionality
    this.logger.info('MCP stdio mode - WebSocket server disabled (not needed for stdio transport)');

    // Initialize smart router without WebSocket server
    initializeSmartRouter({
      redisProducer,
      mageAgentClient,
      websocketServer: null as any, // WebSocket not used in MCP stdio mode
      postgresUrl: config.postgresUrl,
      qdrantUrl: config.qdrantUrl,
      graphragUrl: config.graphragUrl,
    });

    this.routerInitialized = true;
    this.logger.info('Smart router initialized successfully', config);
  }

  /**
   * Setup MCP request handlers
   */
  private setupHandlers(): void {
    // List available tools
    this.server.setRequestHandler(ListToolsRequestSchema, async () => {
      this.logger.debug('Listing tools', { count: VIDEOAGENT_TOOLS.length });

      return {
        tools: VIDEOAGENT_TOOLS,
      };
    });

    // Execute tool via smart-router
    this.server.setRequestHandler(CallToolRequestSchema, async (request) => {
      const { name, arguments: args } = request.params;

      try {
        this.logger.info(`Tool call: ${name}`, {
          args: JSON.stringify(args || {}).substring(0, 200),
        });

        // Ensure router is initialized
        if (!this.routerInitialized) {
          throw new Error('Smart router not initialized. Call initializeRouter() first.');
        }

        // CRITICAL: Route via VideoAgent's smart-router
        // This is the SAME routing used by HTTP REST and WebSocket!
        // No version skew possible because there's only ONE routing implementation.
        const routeDecision = await smartRouter.route(name, args || {});

        this.logger.debug(`Route decision for ${name}:`, {
          service: routeDecision.service,
          endpoint: routeDecision.endpoint,
          method: routeDecision.method,
        });

        // Execute via the selected handler
        const result = await routeDecision.handler(args || {});

        this.logger.info(`Tool ${name} succeeded`, {
          resultType: typeof result,
          resultSize: JSON.stringify(result).length,
        });

        // Format response for MCP protocol
        return {
          content: [
            {
              type: 'text',
              text: JSON.stringify(result, null, 2),
            },
          ],
        };
      } catch (error) {
        this.logger.error(`Tool ${name} failed`, {
          error: error instanceof Error ? error.message : 'Unknown error',
          stack: error instanceof Error ? error.stack : undefined,
        });

        // Return error in MCP format
        return {
          content: [
            {
              type: 'text',
              text: JSON.stringify(
                {
                  success: false,
                  error: error instanceof Error ? error.message : 'Unknown error',
                  tool: name,
                },
                null,
                2
              ),
            },
          ],
          isError: true,
        };
      }
    });
  }

  /**
   * Setup error handlers for MCP server
   */
  private setupErrorHandlers(): void {
    this.server.onerror = (error) => {
      this.logger.error('MCP Server error', {
        error: error.message,
        stack: error.stack,
      });
    };

    // Handle process errors
    process.on('uncaughtException', (error) => {
      this.logger.error('Uncaught exception', {
        error: error.message,
        stack: error.stack,
      });
      process.exit(1);
    });

    process.on('unhandledRejection', (reason, promise) => {
      this.logger.error('Unhandled rejection', {
        reason: String(reason),
        promise: String(promise),
      });
      process.exit(1);
    });
  }

  /**
   * Start MCP adapter with stdio transport
   * Connects to Docker Desktop MCP toolkit
   */
  async start(): Promise<void> {
    this.logger.info('Starting VideoAgent MCP Adapter...');

    // Initialize router before starting
    await this.initializeRouter();

    // Create stdio transport for Docker Desktop
    const transport = new StdioServerTransport();

    this.logger.info('Connecting MCP server to stdio transport...');

    // Connect server to transport
    await this.server.connect(transport);

    this.logger.info('VideoAgent MCP Adapter started successfully', {
      transport: 'stdio',
      tools: VIDEOAGENT_TOOLS.length,
      clients: ['Claude Code', 'Claude Desktop', 'Gemini CLI', 'Codex CLI'],
    });

    // Log tool names for reference
    this.logger.info('Available tools:', {
      tools: VIDEOAGENT_TOOLS.map(t => t.name),
    });
  }

  /**
   * Graceful shutdown
   */
  async stop(): Promise<void> {
    this.logger.info('Stopping VideoAgent MCP Adapter...');

    try {
      await this.server.close();
      this.logger.info('VideoAgent MCP Adapter stopped');
    } catch (error) {
      this.logger.error('Error stopping MCP Adapter', {
        error: error instanceof Error ? error.message : 'Unknown error',
      });
    }
  }
}

// ============================================================================
// Main Execution (if run directly)
// ============================================================================

if (require.main === module) {
  const adapter = new VideoAgentMCPAdapter();

  // Handle graceful shutdown
  process.on('SIGINT', async () => {
    await adapter.stop();
    process.exit(0);
  });

  process.on('SIGTERM', async () => {
    await adapter.stop();
    process.exit(0);
  });

  // Start adapter
  adapter.start().catch((error) => {
    console.error('Failed to start VideoAgent MCP Adapter:', error);
    process.exit(1);
  });
}

// ============================================================================
// Export for programmatic use
// ============================================================================

export { VideoAgentMCPAdapter };
