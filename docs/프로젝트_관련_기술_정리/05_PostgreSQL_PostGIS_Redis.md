# PostgreSQL·PostGIS·Redis Streams

## PostgreSQL/PostGIS

transaction isolation, unique constraint, advisory/row lock, JSONB 사용 경계, GiST 공간 인덱스, geometry와 geography의 차이를 학습한다. route와 patrol area의 버전을 보존하고 migration은 하위 호환 순서로 적용한다.

## Redis Streams

consumer group, pending entries, claim, trim, idempotent consumer를 학습한다. Redis는 전달 계층이며 임무 원장이 아니다. 재처리 가능 기간과 object storage 원본 보존 기간을 명시한다.

## 검증

DB 커밋 직후 프로세스 종료, 동일 이벤트 재수신, consumer 장기 정지, 공간 경계점과 잘못된 좌표를 시험한다.
