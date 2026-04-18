// Package librespot manages the lifecycle of an external librespot
// subprocess: spawning it with the configured bitrate, backend, and
// device name; watching stdout for connection/state events; piping PCM
// audio to the audio package when backend=="pipe"; and invoking
// user-supplied callbacks on reconnect and inactivity.
//
// Process is the single orchestrator. Start launches the binary and
// begins log scanning; Stop sends SIGTERM and waits. OnReconnect fires
// after librespot re-registers with Spotify (used to transfer playback
// back to the preferred device); OnInactive fires when the device has
// been idle long enough that the UI may want to release it.
package librespot
