// The session client lives in the shared KitClient module (apps/apple/KitShared).
// Re-exporting it keeps every file in this app, and its tests, seeing the
// client's types as they did when it was part of this module.
@_exported import KitClient
