import { google, drive_v3 } from 'googleapis';
import { Readable } from 'stream';
import { GoogleOAuthManager } from './GoogleOAuthManager';
import { GoogleDriveFile, GoogleDriveFolder } from '../types';

/**
 * GoogleDriveProvider - Handles Google Drive file operations
 * Supports streaming downloads, folder traversal, and batch processing
 */
export class GoogleDriveProvider {
  private oauthManager: GoogleOAuthManager;
  private drive: drive_v3.Drive | null = null;

  // Google Workspace MIME types
  private readonly GOOGLE_DOC_MIME = 'application/vnd.google-apps.document';
  private readonly GOOGLE_SHEET_MIME = 'application/vnd.google-apps.spreadsheet';
  private readonly GOOGLE_SLIDES_MIME = 'application/vnd.google-apps.presentation';
  private readonly GOOGLE_FOLDER_MIME = 'application/vnd.google-apps.folder';

  // Export MIME types for Google Workspace files
  private readonly EXPORT_MIME_TYPES: Record<string, string> = {
    [this.GOOGLE_DOC_MIME]: 'application/pdf',
    [this.GOOGLE_SHEET_MIME]: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    [this.GOOGLE_SLIDES_MIME]: 'application/pdf',
  };

  // Video MIME types
  private readonly VIDEO_MIME_TYPES = [
    'video/mp4',
    'video/avi',
    'video/mov',
    'video/quicktime',
    'video/webm',
    'video/mkv',
    'video/x-matroska',
    'video/x-msvideo',
  ];

  constructor(oauthManager: GoogleOAuthManager) {
    this.oauthManager = oauthManager;
  }

  /**
   * Initialize Drive API client
   */
  private async initializeDrive(): Promise<void> {
    if (this.drive) {
      return;
    }

    const auth = await this.oauthManager.getAuthenticatedClient();
    this.drive = google.drive({ version: 'v3', auth });
  }

  /**
   * Get file metadata by ID
   */
  async getFileMetadata(fileId: string): Promise<GoogleDriveFile> {
    await this.initializeDrive();

    try {
      const response = await this.drive!.files.get({
        fileId,
        fields: 'id, name, mimeType, size, webViewLink, thumbnailLink',
      });

      const file = response.data;

      return {
        id: file.id!,
        name: file.name!,
        mimeType: file.mimeType!,
        size: parseInt(file.size || '0'),
        webViewLink: file.webViewLink ?? undefined,
        thumbnailLink: file.thumbnailLink ?? undefined,
      };
    } catch (error) {
      throw new Error(`Failed to get file metadata: ${error instanceof Error ? error.message : String(error)}`);
    }
  }

  /**
   * Stream download file from Google Drive
   * Returns a readable stream for memory-efficient processing
   */
  async streamDownload(fileId: string): Promise<Readable> {
    await this.initializeDrive();

    try {
      const metadata = await this.getFileMetadata(fileId);

      // Check if file is a Google Workspace file that needs export
      if (this.isGoogleWorkspaceFile(metadata.mimeType)) {
        return this.exportGoogleWorkspaceFile(fileId, metadata.mimeType);
      }

      // Regular file download
      const response = await this.drive!.files.get(
        {
          fileId,
          alt: 'media',
        },
        { responseType: 'stream' }
      );

      return response.data as Readable;
    } catch (error) {
      throw new Error(`Failed to download file: ${error instanceof Error ? error.message : String(error)}`);
    }
  }

  /**
   * Download file to buffer (for smaller files)
   */
  async downloadToBuffer(fileId: string): Promise<Buffer> {
    const stream = await this.streamDownload(fileId);

    return new Promise((resolve, reject) => {
      const chunks: Buffer[] = [];

      stream.on('data', (chunk) => chunks.push(chunk));
      stream.on('end', () => resolve(Buffer.concat(chunks)));
      stream.on('error', reject);
    });
  }

  /**
   * List files in a folder
   */
  async listFilesInFolder(
    folderId: string,
    pageSize: number = 100
  ): Promise<GoogleDriveFile[]> {
    await this.initializeDrive();

    try {
      const response = await this.drive!.files.list({
        q: `'${folderId}' in parents and trashed = false`,
        pageSize,
        fields: 'files(id, name, mimeType, size, webViewLink, thumbnailLink)',
      });

      return (response.data.files || []).map((file) => ({
        id: file.id!,
        name: file.name!,
        mimeType: file.mimeType!,
        size: parseInt(file.size || '0'),
        webViewLink: file.webViewLink ?? undefined,
        thumbnailLink: file.thumbnailLink ?? undefined,
      }));
    } catch (error) {
      throw new Error(`Failed to list files: ${error instanceof Error ? error.message : String(error)}`);
    }
  }

  /**
   * Recursively discover all files in a folder (including subfolders)
   */
  async discoverFolder(
    folderId: string,
    options: {
      maxDepth?: number;
      maxFiles?: number;
      videoOnly?: boolean;
    } = {}
  ): Promise<{ files: GoogleDriveFile[]; totalSize: number }> {
    const { maxDepth = 5, maxFiles = 1000, videoOnly = true } = options;

    const allFiles: GoogleDriveFile[] = [];
    let totalSize = 0;

    const traverse = async (currentFolderId: string, depth: number): Promise<void> => {
      if (depth > maxDepth || allFiles.length >= maxFiles) {
        return;
      }

      const items = await this.listFilesInFolder(currentFolderId);

      for (const item of items) {
        if (allFiles.length >= maxFiles) {
          break;
        }

        // If it's a folder, traverse it
        if (item.mimeType === this.GOOGLE_FOLDER_MIME) {
          await traverse(item.id, depth + 1);
        }
        // If it's a file and matches criteria
        else {
          const isVideo = this.isVideoFile(item.mimeType);

          if (!videoOnly || isVideo) {
            allFiles.push(item);
            totalSize += item.size;
          }
        }
      }
    };

    await traverse(folderId, 0);

    return { files: allFiles, totalSize };
  }

  /**
   * Get folder structure (for display/confirmation)
   */
  async getFolderStructure(folderId: string, maxDepth: number = 3): Promise<GoogleDriveFolder> {
    await this.initializeDrive();

    const buildStructure = async (currentFolderId: string, depth: number): Promise<GoogleDriveFolder> => {
      const metadata = await this.getFileMetadata(currentFolderId);
      const items = await this.listFilesInFolder(currentFolderId);

      const files: GoogleDriveFile[] = [];
      const folders: GoogleDriveFolder[] = [];

      for (const item of items) {
        if (item.mimeType === this.GOOGLE_FOLDER_MIME) {
          if (depth < maxDepth) {
            const subfolder = await buildStructure(item.id, depth + 1);
            folders.push(subfolder);
          }
        } else {
          files.push(item);
        }
      }

      return {
        id: metadata.id,
        name: metadata.name,
        files,
        folders,
      };
    };

    return buildStructure(folderId, 0);
  }

  /**
   * Extract file ID from Google Drive URL
   */
  extractFileId(url: string): string | null {
    // Match various Google Drive URL formats
    const patterns = [
      /drive\.google\.com\/file\/d\/([a-zA-Z0-9_-]+)/,
      /drive\.google\.com\/open\?id=([a-zA-Z0-9_-]+)/,
      /drive\.google\.com\/folders\/([a-zA-Z0-9_-]+)/,
    ];

    for (const pattern of patterns) {
      const match = url.match(pattern);
      if (match) {
        return match[1];
      }
    }

    return null;
  }

  /**
   * Check if URL is a Google Drive URL
   */
  isGoogleDriveUrl(url: string): boolean {
    return url.includes('drive.google.com');
  }

  /**
   * Check if file is a video
   */
  private isVideoFile(mimeType: string): boolean {
    return this.VIDEO_MIME_TYPES.includes(mimeType);
  }

  /**
   * Check if file is a Google Workspace file
   */
  private isGoogleWorkspaceFile(mimeType: string): boolean {
    return mimeType in this.EXPORT_MIME_TYPES;
  }

  /**
   * Export Google Workspace file (Docs, Sheets, Slides)
   */
  private async exportGoogleWorkspaceFile(fileId: string, mimeType: string): Promise<Readable> {
    const exportMimeType = this.EXPORT_MIME_TYPES[mimeType];

    if (!exportMimeType) {
      throw new Error(`Unsupported Google Workspace file type: ${mimeType}`);
    }

    const response = await this.drive!.files.export(
      {
        fileId,
        mimeType: exportMimeType,
      },
      { responseType: 'stream' }
    );

    return response.data as Readable;
  }

  /**
   * Check if folder (returns true for folders)
   */
  async isFolder(fileId: string): Promise<boolean> {
    const metadata = await this.getFileMetadata(fileId);
    return metadata.mimeType === this.GOOGLE_FOLDER_MIME;
  }

  /**
   * Validate file size
   */
  async validateFileSize(fileId: string, maxSize: number): Promise<{ valid: boolean; size: number }> {
    const metadata = await this.getFileMetadata(fileId);
    return {
      valid: metadata.size <= maxSize,
      size: metadata.size,
    };
  }
}
