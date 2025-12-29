import { Router } from 'express';
import { v4 as uuidv4 } from 'uuid';
import { GoogleOAuthManager } from '../services/GoogleOAuthManager';
import { GoogleDriveProvider } from '../services/GoogleDriveProvider';
import { IQueueAdapter } from '../queue/IQueueAdapter';
import { asyncHandler, ApiError } from '../middleware/errorHandler';
import { JobPayload, ProcessingOptions } from '../types';

/**
 * Setup Google Drive routes
 */
export function setupGoogleDriveRoutes(
  oauthManager: GoogleOAuthManager,
  driveProvider: GoogleDriveProvider,
  queueAdapter: IQueueAdapter
): Router {
  const router = Router();

  /**
   * GET /api/gdrive/auth/url
   * Get OAuth2 authorization URL
   */
  router.get('/auth/url', asyncHandler(async (req, res) => {
    const authUrl = oauthManager.getAuthUrl();

    res.json({
      success: true,
      authUrl,
      message: 'Visit this URL to authorize access to Google Drive',
    });
  }));

  /**
   * POST /api/gdrive/auth/callback
   * Handle OAuth2 callback and exchange code for tokens
   */
  router.post('/auth/callback', asyncHandler(async (req, res) => {
    const { code } = req.body;

    if (!code) {
      throw new ApiError(400, 'Authorization code is required');
    }

    const tokens = await oauthManager.exchangeCodeForTokens(code);

    res.json({
      success: true,
      message: 'Successfully authenticated with Google Drive',
      expiresIn: tokens.expiresIn,
    });
  }));

  /**
   * GET /api/gdrive/auth/status
   * Check if user has valid Google Drive tokens
   */
  router.get('/auth/status', asyncHandler(async (req, res) => {
    const hasTokens = await oauthManager.hasValidTokens();

    res.json({
      success: true,
      authenticated: hasTokens,
      message: hasTokens ? 'User is authenticated' : 'User needs to authenticate',
    });
  }));

  /**
   * DELETE /api/gdrive/auth
   * Logout and delete tokens
   */
  router.delete('/auth', asyncHandler(async (req, res) => {
    await oauthManager.deleteTokens();

    res.json({
      success: true,
      message: 'Successfully logged out from Google Drive',
    });
  }));

  /**
   * GET /api/gdrive/file/:fileId/metadata
   * Get file metadata
   */
  router.get('/file/:fileId/metadata', asyncHandler(async (req, res) => {
    const { fileId } = req.params;

    const metadata = await driveProvider.getFileMetadata(fileId);

    res.json({
      success: true,
      file: metadata,
    });
  }));

  /**
   * POST /api/gdrive/process/file
   * Process a single Google Drive video file
   */
  router.post('/process/file', asyncHandler(async (req, res) => {
    const { fileId, userId, sessionId, options } = req.body;

    if (!fileId) {
      throw new ApiError(400, 'fileId is required');
    }

    if (!userId) {
      throw new ApiError(400, 'userId is required');
    }

    if (!options) {
      throw new ApiError(400, 'Processing options are required');
    }

    // Get file metadata
    const metadata = await driveProvider.getFileMetadata(fileId);

    // Validate file size (2GB limit)
    const maxSize = 2 * 1024 * 1024 * 1024;
    const validation = await driveProvider.validateFileSize(fileId, maxSize);
    if (!validation.valid) {
      throw new ApiError(400, `File too large: ${validation.size} bytes (max: ${maxSize} bytes)`);
    }

    // Download file to buffer
    console.log(`Downloading Google Drive file: ${metadata.name} (${metadata.id})`);
    const videoBuffer = await driveProvider.downloadToBuffer(fileId);

    // Create job
    const jobId = uuidv4();
    const job: JobPayload = {
      jobId,
      filename: metadata.name,
      videoUrl: `https://drive.google.com/file/d/${fileId}`,
      videoBuffer,
      sourceType: 'gdrive',
      userId,
      sessionId,
      options: options as ProcessingOptions,
      metadata: {
        gdriveFileId: fileId,
        gdriveFileName: metadata.name,
        gdriveMimeType: metadata.mimeType,
      },
      enqueuedAt: new Date(),
    };

    // Enqueue job using BullMQ adapter (cast to JobData - videoUrl is guaranteed present for gdrive)
    await queueAdapter.enqueue(job as any);

    res.json({
      success: true,
      jobId,
      fileName: metadata.name,
      fileSize: validation.size,
      status: 'enqueued',
      message: 'Google Drive video processing job enqueued successfully',
    });
  }));

  /**
   * POST /api/gdrive/discover/folder
   * Discover all videos in a Google Drive folder
   */
  router.post('/discover/folder', asyncHandler(async (req, res) => {
    const { folderId, maxDepth, maxFiles, videoOnly } = req.body;

    if (!folderId) {
      throw new ApiError(400, 'folderId is required');
    }

    // Check if it's actually a folder
    const isFolder = await driveProvider.isFolder(folderId);
    if (!isFolder) {
      throw new ApiError(400, 'The provided ID is not a folder');
    }

    // Discover files
    console.log(`Discovering folder: ${folderId}`);
    const discovery = await driveProvider.discoverFolder(folderId, {
      maxDepth: maxDepth || 5,
      maxFiles: maxFiles || 1000,
      videoOnly: videoOnly !== false, // Default to true
    });

    res.json({
      success: true,
      folderId,
      filesFound: discovery.files.length,
      totalSize: discovery.totalSize,
      files: discovery.files,
      message: 'Folder discovery complete',
    });
  }));

  /**
   * POST /api/gdrive/process/folder
   * Process all videos in a Google Drive folder
   */
  router.post('/process/folder', asyncHandler(async (req, res) => {
    const { folderId, userId, sessionId, options, maxFiles, skipConfirmation } = req.body;

    if (!folderId) {
      throw new ApiError(400, 'folderId is required');
    }

    if (!userId) {
      throw new ApiError(400, 'userId is required');
    }

    if (!options) {
      throw new ApiError(400, 'Processing options are required');
    }

    // Check if it's a folder
    const isFolder = await driveProvider.isFolder(folderId);
    if (!isFolder) {
      throw new ApiError(400, 'The provided ID is not a folder');
    }

    // Discover files
    console.log(`Processing folder: ${folderId}`);
    const discovery = await driveProvider.discoverFolder(folderId, {
      maxFiles: maxFiles || 100,
      videoOnly: true,
    });

    if (discovery.files.length === 0) {
      throw new ApiError(404, 'No video files found in folder');
    }

    // If skip confirmation, process immediately
    if (skipConfirmation) {
      const jobIds: string[] = [];

      for (const file of discovery.files) {
        try {
          // Download and enqueue each file
          const videoBuffer = await driveProvider.downloadToBuffer(file.id);
          const jobId = uuidv4();

          const job: JobPayload = {
            jobId,
            filename: file.name,
            videoUrl: `https://drive.google.com/file/d/${file.id}`,
            videoBuffer,
            sourceType: 'gdrive',
            userId,
            sessionId,
            options: options as ProcessingOptions,
            metadata: {
              gdriveFileId: file.id,
              gdriveFileName: file.name,
              gdriveMimeType: file.mimeType,
              folderProcessing: true,
            },
            enqueuedAt: new Date(),
          };

          await queueAdapter.enqueue(job as any);
          jobIds.push(jobId);

          console.log(`Enqueued: ${file.name} (job: ${jobId})`);
        } catch (error) {
          console.error(`Failed to process file ${file.name}:`, error);
        }
      }

      res.json({
        success: true,
        folderId,
        filesProcessed: jobIds.length,
        totalFiles: discovery.files.length,
        jobIds,
        message: 'Folder processing jobs enqueued successfully',
      });
    } else {
      // Return files for confirmation
      res.json({
        success: true,
        folderId,
        filesFound: discovery.files.length,
        totalSize: discovery.totalSize,
        files: discovery.files,
        message: 'Files discovered. Send POST request with skipConfirmation=true to process.',
      });
    }
  }));

  /**
   * POST /api/gdrive/extract-file-id
   * Extract file ID from Google Drive URL
   */
  router.post('/extract-file-id', asyncHandler(async (req, res) => {
    const { url } = req.body;

    if (!url) {
      throw new ApiError(400, 'url is required');
    }

    const fileId = driveProvider.extractFileId(url);

    if (!fileId) {
      throw new ApiError(400, 'Invalid Google Drive URL');
    }

    res.json({
      success: true,
      url,
      fileId,
    });
  }));

  /**
   * GET /api/gdrive/folder/:folderId/structure
   * Get folder structure for display
   */
  router.get('/folder/:folderId/structure', asyncHandler(async (req, res) => {
    const { folderId } = req.params;
    const maxDepth = parseInt(req.query.maxDepth as string) || 3;

    const structure = await driveProvider.getFolderStructure(folderId, maxDepth);

    res.json({
      success: true,
      structure,
    });
  }));

  return router;
}
