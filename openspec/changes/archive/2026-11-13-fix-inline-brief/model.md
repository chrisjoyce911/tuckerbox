# Model

## Skipped

Single writer, request-response only: one engine process writes one brief
file and starts one agent with the brief in its arguments or on its
standard input. No state is shared between actors, and nothing is retried
or handed off.
