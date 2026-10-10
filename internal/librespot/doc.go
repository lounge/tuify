// Package librespot manages the lifecycle of an external librespot
// subprocess: spawning it with the configured bitrate, backend, and
// device name; watching its output for connection and session events; and
// restarting it when it dies.
//
// Process is the single orchestrator. Start launches the binary; Stop sends
// an interrupt (SIGINT), waits up to 5s, then kills it, and suppresses any
// further restarts.
//
// Output: the launch command is logged with credential values (the
// --username argument) redacted. stderr is logged and scanned for
// events. OnReconnect fires after
// librespot authenticates (initially and after every restart; used to
// transfer playback back to the preferred device); OnInactive fires when
// the device has been idle long enough that the UI may want to release it;
// a session that stops producing audio is killed so the restart can
// recover it. Lines still arriving from a previous child after a restart
// are ignored. stdout is handed to OnStdout when set (bootstrap wires it to
// the audio package's PipeReader for the "pipe" backend), otherwise it is
// logged. Output librespot writes just before exiting is logged before the
// exit is.
//
// Restarts: if the process exits without Stop, it is relaunched after a
// delay between 2s and 30s: the sooner after starting it died, the longer
// the wait; a run that stayed up for a minute restarts after 2s. A relaunch
// that fails outright (the binary cannot be started) is retried every 30s
// until Stop. A Stop that lands while a relaunch is pending ends the cycle
// quietly; it is not logged as a failed relaunch.
//
// On Linux the child also gets a parent-death signal, so it is terminated
// even if tuify is killed without running Stop.
package librespot
