# SentinelGrid

SentinelGrid의 첫 번째 수직 슬라이스인 **Operational Picture**를 위한 초기 개발 환경입니다.
설정 기준 문서는 [docs/01_초기_개발환경_세팅.md](docs/01_초기_개발환경_세팅.md)입니다.

## 구성

- `apps/console`: React, TypeScript, TanStack Query, Zod, MapLibre GL
- `services/mission-api`: Go HTTP API (`/health/live`, `/health/ready`, `/api/v1/operational-picture`)
- `database`: PostgreSQL/PostGIS migration, seed, initdb 로더 스크립트
- `contracts`: API 계약(JSON Schema)과 예시 응답
- `docs`: 기획, ADR, 시나리오 문서

pnpm workspace 루트는 저장소 최상위이며, `apps/*`가 워크스페이스 대상입니다.

## 사전 요구사항

| 도구 | 검증 버전 |
| --- | --- |
| Node.js | 22.23.4 |
| pnpm | 10.22.0 (`corepack enable pnpm`으로 활성화) |
| Go | 1.27.4 (`go.mod`는 1.23 이상 요구) |
| Docker Desktop / Compose | 28.3.2 / v2.38.2 |

```powershell
corepack enable pnpm   # pnpm 명령을 PATH에 등록 (관리자 권한 필요)
```

## 최초 1회 준비

```powershell
Copy-Item .env.example .env   # 비밀값은 커밋하지 않습니다
pnpm install                  # 저장소 루트에서 실행
```

## 실행

### 1. PostgreSQL/PostGIS

```powershell
docker compose up -d --wait postgres
docker compose ps
```

`--wait`가 반환되면 migration과 seed가 모두 적용된 상태입니다. healthcheck가 TCP 접속과
`patrol_unit_snapshots` 조회까지 확인하므로, 초기화 중인 임시 서버를 준비 완료로 오인하지 않습니다.

migration이나 seed를 수정한 뒤에는 볼륨을 비워야 다시 적용됩니다. initdb 스크립트는 데이터
디렉터리가 빈 경우에만 실행되기 때문입니다.

```powershell
docker compose down -v
docker compose up -d --wait postgres
```

### 2. Go Mission API

```powershell
cd services\mission-api
go run ./cmd/api
```

기본 포트는 `8080`이며, 현재 응답은 in-memory fixture입니다. PostgreSQL 연결은 Day01-02
슬라이스에서 `internal/platform/postgres`에 구현합니다. 따라서 `/health/ready`는 아직
데이터베이스 상태를 반영하지 않습니다.

### 3. React 콘솔

```powershell
pnpm dev     # 저장소 루트에서 실행. apps/console에서는 pnpm dev 그대로
```

브라우저에서 `http://localhost:5173`을 엽니다. Vite는 IPv6 루프백(`::1`)에 바인딩되므로
`localhost`로 접속하세요. `VITE_API_BASE_URL`은 저장소 루트의 `.env`에서 읽습니다
(`vite.config.ts`의 `envDir`).

## 테스트

```powershell
pnpm test                          # vitest (워크스페이스 루트에서 실행 가능)
pnpm build                         # tsc -b + vite build (strict 타입 검사 포함)
cd services\mission-api; go test ./...
cd services\mission-api; go vet ./...
```

## 기준 문서와의 차이

- 소스코드가 WSL2가 아닌 Windows 경로(`C:\Dev\sideproject\Drone & Air`)에 있습니다.
- 기준 문서 §7의 compose 예시는 `database/migrations`를 `/docker-entrypoint-initdb.d`
  하위에 직접 마운트하지만, postgres 엔트리포인트는 마운트된 **디렉터리를 무시**합니다
  (`ignoring /docker-entrypoint-initdb.d/10-migrations`). 그래서 SQL 트리는
  `/sentinel-sql`에 마운트하고 `database/docker/30-apply-sql.sh`가 순서대로 적용합니다.
- 기준 문서 §3 구조에 없는 루트 `package.json`과 `pnpm-workspace.yaml`을 두어 pnpm
  워크스페이스 루트를 명시했습니다.
