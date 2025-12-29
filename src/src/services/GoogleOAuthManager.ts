import { google, Auth } from 'googleapis';
import { Redis } from 'ioredis';
import { Pool } from 'pg';

/**
 * GoogleOAuthManager - Manages Google OAuth2 authentication and token refresh
 * Stores tokens in Redis (cache) and PostgreSQL (persistence)
 */
export class GoogleOAuthManager {
  private oauth2Client: Auth.OAuth2Client;
  private redis: Redis;
  private pgPool: Pool;
  private userId: string;

  constructor(
    clientId: string,
    clientSecret: string,
    redirectUri: string,
    redisUrl: string,
    postgresUrl: string,
    userId: string = 'default'
  ) {
    this.oauth2Client = new google.auth.OAuth2(clientId, clientSecret, redirectUri);
    this.redis = new Redis(redisUrl);
    this.pgPool = new Pool({ connectionString: postgresUrl });
    this.userId = userId;
  }

  /**
   * Generate OAuth2 authorization URL
   */
  getAuthUrl(): string {
    return this.oauth2Client.generateAuthUrl({
      access_type: 'offline',
      scope: [
        'https://www.googleapis.com/auth/drive.readonly',
        'https://www.googleapis.com/auth/drive.metadata.readonly',
      ],
      prompt: 'consent', // Force consent to get refresh token
    });
  }

  /**
   * Exchange authorization code for tokens
   */
  async exchangeCodeForTokens(code: string): Promise<{
    accessToken: string;
    refreshToken: string;
    expiresIn: number;
  }> {
    try {
      const { tokens } = await this.oauth2Client.getToken(code);

      if (!tokens.access_token) {
        throw new Error('No access token received');
      }

      // Store tokens
      await this.saveTokens(tokens);

      return {
        accessToken: tokens.access_token,
        refreshToken: tokens.refresh_token || '',
        expiresIn: tokens.expiry_date || 3600,
      };
    } catch (error) {
      throw new Error(`Token exchange failed: ${error instanceof Error ? error.message : String(error)}`);
    }
  }

  /**
   * Get valid access token (refreshes if needed)
   */
  async getAccessToken(): Promise<string> {
    // Try to load from cache first
    const cachedToken = await this.loadTokensFromCache();
    if (cachedToken) {
      // Check if expired
      if (cachedToken.expiry_date && cachedToken.expiry_date > Date.now() + 60000) {
        return cachedToken.access_token!;
      }
    }

    // Load from PostgreSQL
    const tokens = await this.loadTokensFromDb();
    if (!tokens) {
      throw new Error('No tokens found. User needs to authenticate.');
    }

    // Set tokens in OAuth2 client
    this.oauth2Client.setCredentials(tokens);

    // Check if token needs refresh
    if (tokens.expiry_date && tokens.expiry_date <= Date.now() + 60000) {
      await this.refreshAccessToken();
      const newTokens = this.oauth2Client.credentials;
      return newTokens.access_token!;
    }

    // Cache the token
    await this.cacheTokens(tokens);

    return tokens.access_token!;
  }

  /**
   * Refresh access token using refresh token
   */
  async refreshAccessToken(): Promise<void> {
    try {
      const { credentials } = await this.oauth2Client.refreshAccessToken();
      await this.saveTokens(credentials);
    } catch (error) {
      throw new Error(`Token refresh failed: ${error instanceof Error ? error.message : String(error)}`);
    }
  }

  /**
   * Get OAuth2 client with valid credentials
   */
  async getAuthenticatedClient(): Promise<Auth.OAuth2Client> {
    await this.getAccessToken(); // This ensures tokens are fresh
    return this.oauth2Client;
  }

  /**
   * Save tokens to Redis (cache) and PostgreSQL (persistence)
   */
  private async saveTokens(tokens: Auth.Credentials): Promise<void> {
    const tokenData = {
      access_token: tokens.access_token,
      refresh_token: tokens.refresh_token,
      expiry_date: tokens.expiry_date,
      token_type: tokens.token_type,
      scope: tokens.scope,
    };

    // Save to Redis with TTL
    const cacheKey = `gdrive:tokens:${this.userId}`;
    await this.redis.setex(
      cacheKey,
      3600, // 1 hour TTL
      JSON.stringify(tokenData)
    );

    // Save to PostgreSQL for persistence
    await this.saveTokensToDb(tokenData);

    // Update OAuth2 client credentials
    this.oauth2Client.setCredentials(tokenData);
  }

  /**
   * Load tokens from Redis cache
   */
  private async loadTokensFromCache(): Promise<Auth.Credentials | null> {
    try {
      const cacheKey = `gdrive:tokens:${this.userId}`;
      const cached = await this.redis.get(cacheKey);

      if (!cached) {
        return null;
      }

      return JSON.parse(cached);
    } catch (error) {
      return null;
    }
  }

  /**
   * Cache tokens in Redis
   */
  private async cacheTokens(tokens: Auth.Credentials): Promise<void> {
    const cacheKey = `gdrive:tokens:${this.userId}`;
    await this.redis.setex(cacheKey, 3600, JSON.stringify(tokens));
  }

  /**
   * Save tokens to PostgreSQL
   */
  private async saveTokensToDb(tokens: Auth.Credentials): Promise<void> {
    const query = `
      INSERT INTO videoagent.gdrive_tokens (user_id, access_token, refresh_token, expiry_date, token_type, scope, updated_at)
      VALUES ($1, $2, $3, $4, $5, $6, NOW())
      ON CONFLICT (user_id) DO UPDATE SET
        access_token = EXCLUDED.access_token,
        refresh_token = EXCLUDED.refresh_token,
        expiry_date = EXCLUDED.expiry_date,
        token_type = EXCLUDED.token_type,
        scope = EXCLUDED.scope,
        updated_at = NOW()
    `;

    await this.pgPool.query(query, [
      this.userId,
      tokens.access_token,
      tokens.refresh_token,
      tokens.expiry_date ? new Date(tokens.expiry_date) : null,
      tokens.token_type,
      tokens.scope,
    ]);
  }

  /**
   * Load tokens from PostgreSQL
   */
  private async loadTokensFromDb(): Promise<Auth.Credentials | null> {
    const query = `
      SELECT access_token, refresh_token, expiry_date, token_type, scope
      FROM videoagent.gdrive_tokens
      WHERE user_id = $1
    `;

    const result = await this.pgPool.query(query, [this.userId]);

    if (result.rows.length === 0) {
      return null;
    }

    const row = result.rows[0];
    return {
      access_token: row.access_token,
      refresh_token: row.refresh_token,
      expiry_date: row.expiry_date ? new Date(row.expiry_date).getTime() : undefined,
      token_type: row.token_type,
      scope: row.scope,
    };
  }

  /**
   * Delete tokens (logout)
   */
  async deleteTokens(): Promise<void> {
    // Delete from cache
    const cacheKey = `gdrive:tokens:${this.userId}`;
    await this.redis.del(cacheKey);

    // Delete from PostgreSQL
    const query = `DELETE FROM videoagent.gdrive_tokens WHERE user_id = $1`;
    await this.pgPool.query(query, [this.userId]);
  }

  /**
   * Check if user has valid tokens
   */
  async hasValidTokens(): Promise<boolean> {
    try {
      const tokens = await this.loadTokensFromDb();
      return tokens !== null && !!tokens.access_token;
    } catch (error) {
      return false;
    }
  }

  /**
   * Close connections
   */
  async close(): Promise<void> {
    await this.redis.quit();
    await this.pgPool.end();
  }
}
