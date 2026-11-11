# React·TypeScript 관제 UI

## 1. 화면 구성

- Operations Overview: 임무·기체·Incident 요약
- Patrol Map: 구역·Route·Checkpoint·Coverage·미확인 구간
- Asset Detail: 위치·자세·배터리·통신·Freshness
- Mission Control: 배정·진행·Hold·Resume·Return·Abort
- Incident Center: 경보 묶음·근거·조치·해제
- Replay Timeline: Telemetry·Command·운영자 조작 재생
- AI Evidence: 확인 사실·가설·근거 문서·모델 버전

## 2. 상태 관리

- React Query: REST Server State
- Zustand: 선택 기체·지도·재생 위치 등 UI State
- WebSocket Event는 `state_version`과 event time을 검사한다.
- 재접속 시 Snapshot을 받은 뒤 Event Stream을 이어간다.

## 3. 안전 UX

- `NORMAL/WARNING/CRITICAL/UNKNOWN`과 `FRESH/STALE/PARTIAL/INVALID`를 분리한다.
- Command 전송, ACK, 실행, 효과 확인을 다른 단계로 표시한다.
- 마지막 위치는 `last received` 시각과 함께 표시한다.
- 고위험 명령은 영향·만료·정책·승인자를 다시 확인한다.
- AI의 사실과 가설을 다른 시각 표현으로 구분한다.

## 4. 개인정보 최소화

MVP 화면은 개인 식별 정보가 아닌 구역 Event, 객체 수·유형과 시설 상태 후보를 표시한다. 원본 영상 장기 보존을 기본값으로 사용하지 않는다.
