# Fault Isolation System (User Space)

## Quick start

1) Build

```
cd user-space
go build ./...
```

2) Run the demo helper in another terminal

```
go build -o fisdemo ./cmd/fisdemo
./fisdemo -mode cpu
```

3) Run the service

```
go run ./cmd/fis -config configs/fis.json -http :8090
```

4) View status and events

```
go run ./cmd/fisctl status -config configs/fis.json
go run ./cmd/fisctl events -config configs/fis.json -n 20
```

## API + UI dev

1) Start the Go API server

```
go run ./cmd/fis -config configs/fis.json -http :8090
```

2) Start the Vite UI (from your UI project directory)

```
npm install
npm run dev
```

The API listens on `http://localhost:8090` and enables CORS for `http://localhost:5173`.

## API endpoints

- `GET /api/v1/config`
- `PUT /api/v1/config`
- `GET /api/v1/status`
- `GET /api/v1/events?limit=50`
- `GET /api/v1/events/stream`
- `POST /api/v1/demos`
- `DELETE /api/v1/demos/{pid}`

## Configuration

- Edit [configs/fis.json](configs/fis.json) to match your targets and limits.
- `command` is used for restart actions. Use an absolute path for production.
- `cpu_max` uses the cgroup v2 cpu.max format (for example, `50000 100000`).

## Required permissions

- Reading `/proc` usually works without elevated privileges.
- Cgroup v2 operations require root or CAP_SYS_ADMIN. Run the service with sudo or set `cgroup_root` to an empty string to disable cgroup actions.

## Demo notes

- `fisdemo` can simulate CPU spikes, memory growth, or crash behavior.
- Use `-mode cpu`, `-mode mem`, or `-mode crash`.
