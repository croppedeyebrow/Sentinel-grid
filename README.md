# SentinelGrid

완료된 수직 슬라이스는 두 개입니다.

| 슬라이스 | 범위 문서 | 결정 기록 |
| --- | --- | --- |
| Operational Picture (Day 01–02) | [01_운영상황도_슬라이스](docs/개발_과정_14일/01_운영상황도_슬라이스_Day01-02.md) | [ADR 0002](docs/adr/0002-two-axis-unit-status.md) |
| Patrol Mission (Day 03–04) | [02_순찰임무_슬라이스](docs/개발_과정_14일/02_순찰임무_슬라이스_Day03-04.md) | [ADR 0003](docs/adr/0003-patrol-mission-lifecycle-and-coverage.md) |

개발 방식은 [docs/00_도메인_수직슬라이스_개발_가이드.md](docs/00_도메인_수직슬라이스_개발_가이드.md),
초기 환경 기준은 [docs/01_초기_개발환경_세팅.md](docs/01_초기_개발환경_세팅.md)입니다.

## 구성

- `apps/console`: React, TypeScript, TanStack Query, Zod, MapLibre GL
- `services/mission-api`: Go HTTP API (`domain` / `application` / `adapter`)
- `database`: migration, seed, initdb 로더와 fixture 도구
- `contracts`: API 계약(JSON Schema)과 예시 응답
- `docs`: 기획, ADR, 시나리오 문서

pnpm workspace 루트는 저장소 최상위이며, `apps/*`가 워크스페이스 대상입니다.

## 상태 모델

순찰체 상태와 데이터 품질은 **별개의 두 축**입니다. 하나의 필드로 합치지 않습니다.

| 축 | 값 | 의미 |
| --- | --- | --- |
| 기체 상태 `status` | `NORMAL` `WARNING` `CRITICAL` `UNKNOWN` | 순찰체가 정상인가 |
| 관측 신선도 `freshness` (위치·배터리·링크 각각) | `FRESH` `STALE` `MISSING` | 그 판단의 근거가 최신인가 |

관측이 하나라도 `FRESH`가 아니면 상태는 `UNKNOWN`입니다. 근거가 부족할 때 정상도
위험도 단정하지 않습니다. 판정 기준(신선도 창, 배터리·링크 임계값)은 Site Profile에
저장되고 API 응답에 함께 실립니다. 배경과 제한사항은
[docs/adr/0002-two-axis-unit-status.md](docs/adr/0002-two-axis-unit-status.md)에 있습니다.

임무도 같은 방식으로 **생명주기와 판정을 분리**합니다.

| 축 | 값 | 의미 |
| --- | --- | --- |
| 임무 상태 `status` | `DRAFT` `READY` `ACTIVE` `COMPLETED` `ABORTED` | 임무가 어디까지 진행됐는가 |
| 체크포인트 판정 `state` | `PLANNED` `VISITED` `MISSED` `UNKNOWN` | 그 지점이 실제로 순찰됐는가 |

통과는 **신뢰할 수 있는 관측**만 인정합니다. 진행 중인 임무에는 `MISSED`가 없고,
종료된 임무라도 그 시간대에 신뢰 관측이 없었다면 `MISSED`가 아니라 `UNKNOWN`입니다.
`통과 + 미순찰`의 합은 전체와 다를 수 있으며, 화면은 그 간격을 그대로 보여줍니다.
중단은 오류가 아니라 사유를 기록하는 정상 종료입니다. 배경·제한사항·관측 지표는
[docs/adr/0003-patrol-mission-lifecycle-and-coverage.md](docs/adr/0003-patrol-mission-lifecycle-and-coverage.md)에 있습니다.

쓰기 요청은 두 가지 규칙을 따릅니다 — 전이는 호출자가 읽은 `version`을 함께 보내야
하고(낡으면 `STALE_VERSION`으로 거부), 임무 생성은 `Idempotency-Key`를 요구합니다.
엔드포인트와 오류 코드는 [contracts/README.md](contracts/README.md)에 정리돼 있습니다.

## 사전 요구사항

| 도구 | 검증 버전 |
| --- | --- |
| Node.js | 22.23.4 |
| pnpm | 10.22.0 (`corepack enable pnpm`으로 활성화) |
| Go | 1.27.4 (`go.mod`는 1.25 이상 요구) |
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

seed는 초기화 시점의 절대 시각을 기록하므로 시간이 지나면 모든 관측이 `STALE`이 됩니다.
상대적인 나이를 유지한 채 fixture를 현재로 당기려면:

```powershell
docker compose exec -T postgres psql -U sentinel -d sentinel_grid -f /sentinel-sql/tools/refresh_fixture.sql
```

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

- `GET /health/live` — 프로세스 생존. 데이터베이스를 확인하지 않습니다.
- `GET /health/ready` — PostgreSQL 연결 가능 여부를 반영합니다. 불가 시 503.
- `GET /api/v1/operational-picture` — Site Profile, 순찰구역, 경로, 순찰체 관측.

환경변수: `API_PORT`(기본 8080), `POSTGRES_DSN`(기본값은 로컬 Compose 기준),
`CONSOLE_ORIGINS`(쉼표 구분, 기본 `http://localhost:5173,http://localhost:4173`).
연결은 지연 생성되므로 데이터베이스가 내려가 있어도 API는 기동하고 readiness로 보고합니다.

### 3. React 콘솔

```powershell
pnpm dev     # 저장소 루트에서 실행
```

브라우저에서 `http://localhost:5173`을 엽니다. Vite는 IPv6 루프백(`::1`)에 바인딩되므로
`localhost`로 접속하세요. 화면은 두 개이고 서로 링크로 이동합니다.

| 경로 | 화면 |
| --- | --- |
| `/` | 운영상황도 — 순찰체 상태와 관측 신선도 |
| `/missions` | 순찰 임무 — 계획·승인·실행·중단과 coverage | `VITE_API_BASE_URL`은 저장소 루트의 `.env`에서 읽습니다
(`vite.config.ts`의 `envDir`).

## 테스트

```powershell
pnpm test                          # vitest: 계약·UI 모델·화면 상태
pnpm build                         # tsc -b + vite build (strict 타입 검사 포함)
cd services\mission-api; go test ./...          # 통합 시험 포함 (DB 필요)
cd services\mission-api; go test -short ./...   # DB 없이 단위 시험만
cd services\mission-api; go vet ./...
```

Go 시험에는 실제 PostgreSQL을 쓰는 통합 시험이 포함됩니다. 공간 질의, 낙관적 잠금,
DB 제약은 가짜 저장소로 증명할 수 없기 때문입니다. 데이터베이스에 연결할 수 없으면 해당
시험은 **건너뜁니다**(`-short`로 명시적으로 제외 가능). 접속 정보는 `POSTGRES_DSN`을
따르며, 시험이 만든 임무는 종료 시 스스로 삭제합니다.

의존성 중단 시 동작은 [docs/scenarios/patrol-mission.md](docs/scenarios/patrol-mission.md)의
"Representative failure drill" 절차로 확인합니다 — `/health/live`는 200을 유지하고
읽기·쓰기는 503 `PROJECTION_UNAVAILABLE`을 반환하며, 데이터베이스 복구 후 API 재시작 없이
정상화됩니다.

계약은 양쪽에서 같은 파일로 검증합니다. Go 시험은 응답과 커밋된 예시를
`contracts/schemas/operational-picture.schema.json`에 대해 확인하고, 콘솔 시험은 같은
예시를 Zod schema로 파싱합니다. 따라서 한쪽만 바꾸면 반대쪽 시험이 깨집니다.

## 기준 문서와의 차이

- 소스코드가 WSL2가 아닌 Windows 경로(`C:\Dev\sideproject\Drone & Air`)에 있습니다.
- 기준 문서 §7의 compose 예시는 `database/migrations`를 `/docker-entrypoint-initdb.d`
  하위에 직접 마운트하지만, postgres 엔트리포인트는 마운트된 **디렉터리를 무시**합니다
  (`ignoring /docker-entrypoint-initdb.d/10-migrations`). 그래서 SQL 트리는
  `/sentinel-sql`에 마운트하고 `database/docker/30-apply-sql.sh`가 순서대로 적용합니다.
- 기준 문서 §3 구조에 없는 루트 `package.json`과 `pnpm-workspace.yaml`을 두어 pnpm
  워크스페이스 루트를 명시했습니다.
- Day01-02 문서의 `FRESH/STALE/MISSING`을 신선도 축으로, `종합_기획_문서/06_React_관제UI.md`의
  `NORMAL/WARNING/CRITICAL/UNKNOWN`을 상태 축으로 채택했습니다. 초기 세팅 문서와
  이전 시나리오 문서가 쓰던 단일 `NORMAL/STALE/MISSING` enum은 제거했습니다.
- `종합_기획_문서/03_Go_임무API_아키텍처.md` §5는 `/checkpoints`를 독립 엔드포인트로
  적어두었지만, 승인이 계획을 임무 버전에 고정해야 하므로 체크포인트와 배정을 임무
  하위 자원으로 두었습니다. 이유는 ADR 0003에 있습니다.
