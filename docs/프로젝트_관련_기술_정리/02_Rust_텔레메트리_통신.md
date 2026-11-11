# Rust 텔레메트리·통신

## 학습 항목

- ownership·borrowing·lifetime과 `Result` 오류 모델
- Tokio runtime, task cancellation, bounded `mpsc`
- serde/protobuf, framing, timeout, retry·backoff
- 메모리·CPU profiling, property test, fuzzing
- mTLS, 장비 신원, replay 방지의 기본 구조

## 적용 원칙

Rust는 외부 입력을 안전한 내부 이벤트로 바꾸는 경계다. 임무 정책은 판단하지 않으며 스키마, 크기, 신원, 세션, 순서, 우선순위를 검증한다. 수용량을 넘으면 조용히 메모리를 늘리지 않고 정책에 따라 일반 텔레메트리를 감축하고 지표를 남긴다.

## 실습 증거

깨진 frame fuzzing, 느린 consumer, burst traffic, 연결 반복, 명령 결과 우선 전달 시험을 수행한다.
