/**
 * Simple logger utility for VideoAgent
 */

export function createLogger(component: string) {
  return {
    info: (message: string, ...args: any[]) => console.log(`[${component}] INFO:`, message, ...args),
    warn: (message: string, ...args: any[]) => console.warn(`[${component}] WARN:`, message, ...args),
    error: (message: string, ...args: any[]) => console.error(`[${component}] ERROR:`, message, ...args),
    debug: (message: string, ...args: any[]) => console.debug(`[${component}] DEBUG:`, message, ...args),
  };
}
