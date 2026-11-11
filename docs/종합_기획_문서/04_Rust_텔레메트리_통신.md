# Rust 텔레메트리·통신 아키텍처

## 1. 책임

Rust는 신뢰할 수 없는 외부 입력을 검증된 내부 Event로 바꾸는 통신 경계다.

- mTLS와 기체 ID 검증
- Frame 크기·Schema·단위·범위 검사
- Sequence Gap·중복·역순·Replay 탐지
- Command/Telemetry Queue와 Credential 분리
- Bounded Queue·Backpressure·Rate Limit
- Edge Store-and-Forward와 재접속
- Snapshot·last sequence·last command handshake

## 2. 처리 흐름

```text
Connection
→ Device Identity
→ Frame Decode
→ Contract Validation
→ Sequence·Timestamp Check
→ Normalization
→ Telemetry Stream
```

## 3. 경계 규칙

Rust는 통신 품질을 판단하지만 임무 지속·중단을 결정하지 않는다. `battery=18`을 수신하고 검증할 수 있지만, 해당 값으로 순찰을 중단할지는 Go의 Mission Policy가 결정한다.

## 4. 장애 정책

- Queue 상한 도달 시 일반 Telemetry를 병합 또는 폐기한다.
- Command·ACK·긴급 Event는 일반 Telemetry보다 우선한다.
- 재접속에는 exponential backoff와 jitter를 적용한다.
- Store-and-Forward 용량·보존시간·폐기 순서를 기록한다.
- Gateway 장애 시 새 Command Dispatch를 제한하고 미확인 상태를 보존한다.

## 5. 시험

Malformed Frame fuzzing, 인증서 위장·만료·폐기, Replay, 집단 재접속, Telemetry 폭주 중 Command 지연을 시험한다.
