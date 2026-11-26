# SentinelGrid

| 슬라이스 | 상태 | 범위 문서 | 결정 기록 |
| --- | --- | --- | --- |
| Operational Picture (Day 01–02) | 완료 | [01_운영상황도_슬라이스](docs/개발_과정_14일/01_운영상황도_슬라이스_Day01-02.md) | [ADR 0002](docs/adr/0002-two-axis-unit-status.md) |
| Patrol Mission (Day 03–04) | 완료 | [02_순찰임무_슬라이스](docs/개발_과정_14일/02_순찰임무_슬라이스_Day03-04.md) | [ADR 0003](docs/adr/0003-patrol-mission-lifecycle-and-coverage.md) |
| Telemetry Reliability (Day 05–06) | 1단계(수집·투영) 동작 | [03_텔레메트리신뢰성_슬라이스](docs/개발_과정_14일/03_텔레메트리신뢰성_슬라이스_Day05-06.md), [시나리오](docs/scenarios/telemetry-reliability.md) | 2단계와 함께 기록 |

텔레메트리 슬라이스는 두 단계로 나뉩니다. **1단계**는 프레임을 받아 판정하고 투영까지
반영하는 경로(Rust 게이트웨이 → Redis Streams → Go 소비자 → PostgreSQL)이며 지금
동작합니다. **2단계**는 그 변화를 콘솔로 실시간 전달하는 경로(WebSocket)로, 아직
없습니다. 현재 콘솔은 폴링으로 같은 투영을 읽습니다.

개발 방식은 [docs/00_도메인_수직슬라이스_개발_가이드.md](docs/00_도메인_수직슬라이스_개발_가이드.md),
초기 환경 기준은 [docs/01_초기_개발환경_세팅.md](docs/01_초기_개발환경_세팅.md)입니다.

## 구성

- `apps/console`: React, TypeScript, TanStack Query, Zod, MapLibre GL
- `services/mission-api`: Go HTTP API와 텔레메트리 소비자 (`domain` / `application` / `adapter`)
- `services/telemetry-gateway`: Rust 통신 경계 — 프레임 수신·검증·발행
- `python/patrol-simulator`: 결함을 선언해서 주입하는 결정적 순찰체 시뮬레이터
- `database`: migration, seed, initdb 로더와 fixture 도구
- `contracts`: API 계약(JSON Schema)과 예시 응답
- `docs`: 기획, ADR, 시나리오 문서

텔레메트리는 한 방향으로만 흐릅니다.

```
시뮬레이터/순찰체 ──HTTP+Bearer──▶ telemetry-gateway ──Redis Streams──▶ mission-api 소비자 ──▶ PostgreSQL
                                   (검증·판정·영수증)      (telemetry.events)   (투영·수신 기록)        │
                                                                                                      ▼
                                                                              콘솔 ◀── GET /operational-picture
```

프레임을 받는 입구는 게이트웨이 하나뿐입니다. 시뮬레이터는 데이터베이스 자격정보를
갖지 않고, 게이트웨이는 발행할 수 없는 프레임을 받아두지 않습니다.

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

신선도는 **센서마다 따로** 판정합니다. 위치는 20분 전, 배터리는 6초 전에 측정될 수
있고 그 둘은 같은 행에 공존합니다. 그래서 각 관측은 자신의 `observedAt`(측정 시각)과
`receivedAt`(수신 시각)을 함께 싣습니다 — 두 시각을 합치면 "지금 유효한가"와 "받았을
때 믿을 수 있었나"를 구분할 수 없습니다.

수신 상태는 또 하나의 축입니다. 값이 낡은 것과 아무것도 도착하지 않는 것은 다른
사실이기 때문입니다.

| 축 | 값 | 의미 |
| --- | --- | --- |
| 스트림 상태 `stream.state` | `STREAMING` `SILENT` `NEVER_SEEN` | 지금 무언가 도착하고 있는가 |

`stream`은 세션(`sessionId`), 재접속 여부(`reconnected`), 현재 세션에서 빠진 프레임
수(`missingFrames`), 받았지만 쓸 수 없던 프레임 수(`rejectedFrames`)를 함께 보고합니다.
불완전한 스트림은 불완전한 것으로 보입니다 — 끊긴 구간을 보간하지 않습니다.
재접속은 세션을 바꾸되 이미 측정된 값은 버리지 않고, 끝난 세션의 지각 프레임은
스트림 진행을 되돌리지 못합니다(판정은 **측정 시각** 순서로 합니다).

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
| Rust | 1.92.0 (게이트웨이, `cargo`) |
| Python | 3.14 + [uv](https://docs.astral.sh/uv/) 0.9 (시뮬레이터) |
| Docker Desktop / Compose | 28.3.2 / v2.38.2 |

Rust와 Python은 텔레메트리 슬라이스에만 필요합니다. 게이트웨이는 Compose 이미지로도
돌 수 있으므로, 직접 수정하지 않는다면 `cargo` 없이도 전체 스택을 띄울 수 있습니다.

```powershell
corepack enable pnpm   # pnpm 명령을 PATH에 등록 (관리자 권한 필요)
```

## 최초 1회 준비

```powershell
Copy-Item .env.example .env   # 비밀값은 커밋하지 않습니다
pnpm install                  # 저장소 루트에서 실행
```

## 실행

컨테이너로 도는 의존성은 한 번에 띄울 수 있고, `--wait`는 모든 healthcheck가 통과할
때까지 돌아오지 않습니다.

```powershell
docker compose up -d --wait        # postgres, redis, telemetry-gateway
```

Go API와 콘솔은 개발 중 재시작이 잦아 호스트에서 직접 띄웁니다(3·4단계).

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

### 2. Redis와 텔레메트리 게이트웨이

```powershell
docker compose up -d --wait redis telemetry-gateway
docker compose ps
```

게이트웨이는 `http://localhost:8090`에서 받습니다. Redis는 영속화를 끈 상태로
(`--appendonly no --save ""`) 전달 통로로만 씁니다 — 남겨야 할 사실은 PostgreSQL의
투영과 수신 기록이고, 스트림은 그 사이의 운반 수단입니다.

| 환경변수 | 기본값 | 의미 |
| --- | --- | --- |
| `TELEMETRY_BIND_ADDRESS` | `0.0.0.0:8090` | 수신 주소 |
| `TELEMETRY_TOKEN` | — | 필수. 없으면 **기동하지 않습니다** |
| `REDIS_URL` | `redis://redis:6379` | 발행 대상 |
| `POSTGRES_DSN` | Compose 기준 | 순찰체 명부 조회(읽기 전용) |

토큰은 필수이고 기본값이 없습니다. 변수를 잊은 채 인증 없이 프레임을 받는 상태로
기동하는 것보다, 기동하지 않고 드러나는 편이 안전하기 때문입니다. 로컬 기본값은
`.env`의 `TELEMETRY_TOKEN`입니다.

```powershell
curl http://localhost:8090/health/ready    # 발행 가능 여부. 불가 시 503
```

게이트웨이는 발행할 수 없으면 프레임을 받지 않고 `STREAM_UNAVAILABLE`로 거절합니다.
받아둔 뒤 조용히 잃는 것보다 거절이 정직하기 때문입니다.

### 3. Go Mission API

```powershell
cd services\mission-api
go run ./cmd/api
```

- `GET /health/live` — 프로세스 생존. 데이터베이스를 확인하지 않습니다.
- `GET /health/ready` — PostgreSQL 연결 가능 여부를 반영합니다. 불가 시 503.
- `GET /api/v1/operational-picture` — Site Profile, 순찰구역, 경로, 순찰체 관측.
- `/api/v1/missions…` — 순찰 임무 ([contracts/README.md](contracts/README.md)).

같은 프로세스가 텔레메트리 스트림 소비자도 함께 돌립니다. HTTP 서버와 나란히
goroutine으로 기동하며, 적용이 끝난 뒤에 `XACK`하므로 적용 중 죽은 항목은 다시
전달됩니다(같은 영수증을 두 번 적용해도 결과는 같습니다).

| 환경변수 | 기본값 |
| --- | --- |
| `API_PORT` | `8080` |
| `POSTGRES_DSN` | 로컬 Compose 기준 |
| `CONSOLE_ORIGINS` | `http://localhost:5173,http://localhost:4173` (쉼표 구분) |
| `REDIS_URL` | `redis://localhost:6379` |
| `TELEMETRY_STREAM_KEY` | `telemetry.events` |
| `TELEMETRY_CONSUMER_GROUP` | `mission-api` |
| `TELEMETRY_CONSUMER_NAME` | `mission-api-1` |

연결은 지연 생성되므로 데이터베이스가 내려가 있어도 API는 기동하고 readiness로 보고합니다.
소비자 그룹이 사라져 있으면(스트림을 비운 경우) 다시 만들고 이어갑니다.

### 4. React 콘솔

```powershell
pnpm dev     # 저장소 루트에서 실행
```

브라우저에서 `http://localhost:5173`을 엽니다. Vite는 IPv6 루프백(`::1`)에 바인딩되므로
`localhost`로 접속하세요. 화면은 두 개이고 서로 링크로 이동합니다.

| 경로 | 화면 |
| --- | --- |
| `/` | 운영상황도 — 순찰체 상태와 관측 신선도 |
| `/missions` | 순찰 임무 — 계획·승인·실행·중단과 coverage |

`VITE_API_BASE_URL`은 저장소 루트의 `.env`에서 읽습니다 (`vite.config.ts`의 `envDir`).

### 5. 순찰체 시뮬레이터

실제 순찰체 없이 통신 결함을 재현하는 도구입니다. 시나리오 manifest가 결함을
선언하고, 같은 시드는 같은 프레임을 만듭니다 — 재현할 수 없는 결함은 진단할 수
없기 때문입니다.

```powershell
cd python\patrol-simulator
uv sync --extra dev
uv run patrol-simulator scenarios\unreliable-link.yaml --token local-telemetry-token
```

| manifest | 내용 |
| --- | --- |
| `scenarios/nominal.yaml` | 결함 없는 정상 운행 |
| `scenarios/unreliable-link.yaml` | 중복·재배열·유실·지연·끊김·범위초과·미래시각·초과크기 — 시나리오 문서의 실패 표 전체 |

| 옵션 | 의미 |
| --- | --- |
| `--gateway` | 게이트웨이 주소 (기본 `http://localhost:8090`) |
| `--token` | Bearer 토큰. 생략하면 `TELEMETRY_TOKEN`을 읽습니다 |
| `--dry-run` | 보내지 않고 만들 프레임을 출력 |
| `--loop` | 중단할 때까지 반복 (라이브 시연용) |
| `--pause` | 프레임 간 대기 초 |

실행이 끝나면 **주입한 결함**과 **게이트웨이의 판정**을 나란히 출력합니다. 프레임만
보고 비교하면 순환 논증이 되므로, 주입 기록은 프레임과 따로 보관합니다.

```json
{"injected": {"duplicate": 1, "out_of_range": 1, "reorder": 2, "future": 1, "oversized": 1, "delay": 1},
 "outcomes": {"ACCEPTED": 20, "DUPLICATE": 1, "VALUE_OUT_OF_RANGE": 1, "GAP": 6,
              "OUT_OF_ORDER": 2, "MEASURED_IN_FUTURE": 1, "FRAME_TOO_LARGE": 1}}
```

실행 후 투영과 수신 기록을 대조합니다. 투영 카운터는 **현재 세션**의 것이고 수신
기록은 받은 전부이므로, 재접속한 순찰체는 이전 세션의 갭 영수증이 기록에 남은 채
카운터는 0부터 다시 셉니다. 끊김이 없었던 순찰체에서는 두 숫자가 같아야 합니다.

```powershell
docker compose exec -T postgres psql -U sentinel -d sentinel_grid -c @'
SELECT u.callsign, s.last_sequence, s.missing_frames, s.rejected_frames,
       count(r.*) FILTER (WHERE r.outcome = 'GAP') AS gap_receipts
FROM patrol_unit_snapshots s
JOIN patrol_units u ON u.id = s.patrol_unit_id
LEFT JOIN telemetry_receipts r ON r.patrol_unit_id = s.patrol_unit_id
GROUP BY 1, 2, 3, 4 ORDER BY 1;
'@
```

시뮬레이터는 측정 시각을 **과거 구간**에 두고 마지막 tick이 현재에 닿도록 생성합니다.
게이트웨이가 자기 시계보다 앞선 측정을 거절하기 때문에, 현재부터 미래로 생성하면
대부분이 `MEASURED_IN_FUTURE`로 거절됩니다. 또 실행마다 세션 id에 시작 시각을 넣으므로,
같은 manifest를 다시 돌려도 이전 세션의 재전송(`DUPLICATE`)이 되지 않습니다.

## 테스트

```powershell
pnpm test                          # vitest: 계약·UI 모델·화면 상태
pnpm build                         # tsc -b + vite build (strict 타입 검사 포함)
cd services\mission-api; go test ./...          # 통합 시험 포함 (DB 필요)
cd services\mission-api; go test -short ./...   # DB 없이 단위 시험만
cd services\mission-api; go vet ./...
cd services\telemetry-gateway; cargo test       # 계약 왕복 포함
cd services\telemetry-gateway; cargo clippy --all-targets -- -D warnings
cd python\patrol-simulator; uv run pytest -q    # 시뮬레이터 결정성·결함 주입
cd python\patrol-simulator; uv run ruff check .
```

Go 시험에는 실제 PostgreSQL을 쓰는 통합 시험이 포함됩니다. 공간 질의, 낙관적 잠금,
DB 제약은 가짜 저장소로 증명할 수 없기 때문입니다. 데이터베이스에 연결할 수 없으면 해당
시험은 **건너뜁니다**(`-short`로 명시적으로 제외 가능). 접속 정보는 `POSTGRES_DSN`을
따르며, 시험이 만든 임무는 종료 시 스스로 삭제합니다. 텔레메트리 통합 시험은 자기
순찰체를 직접 만들고 지웁니다 — 투영은 순찰체당 한 행이라 시드 fixture를 공유하면
시험끼리 서로의 상태를 읽고, 콘솔이 보는 화면도 흐트러지기 때문입니다.

전체 경로는 시뮬레이터로 한 번에 확인합니다 — 게이트웨이 판정이 주입한 결함과
일치하는지, 투영 카운터가 수신 기록과 일치하는지, 두 가지를 봅니다. 이 대조로
잡은 실제 결함이 있습니다. "끝난 세션의 지각 프레임"과 "새 세션의 첫 프레임"을
수신 시각으로 구분하려 하면 둘을 구분할 수 없습니다 — 측정 시각과 수신 시각은
서로 다른 시계이고, 지연된 프레임은 일찍 측정되어 늦게 도착하기 때문입니다.
판정은 측정 시각끼리 비교합니다.

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
