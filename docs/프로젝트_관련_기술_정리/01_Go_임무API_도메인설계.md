# Go 임무 API와 도메인 설계

## 학습 항목

- `context.Context`, goroutine·channel의 취소와 종료
- `net/http` 또는 경량 router, middleware, validation
- `database/sql` 계열, transaction, isolation, outbox
- interface를 소비 측에서 정의하는 방식과 오류 wrapping
- table-driven test, race detector, fuzz test
- DDD aggregate, repository, application service, domain event

## 적용 원칙

도메인 규칙을 handler와 ORM 모델에 흩뜨리지 않는다. Mission과 Command aggregate가 상태 전이와 불변조건을 소유하고 adapter가 PostgreSQL·Redis·HTTP를 연결한다. 무제한 goroutine을 만들지 않고 worker와 queue에 명시적 상한을 둔다.

## 실습 증거

동시 control lease 획득, 같은 idempotency key 재요청, outbox 재발행을 race test와 통합시험으로 검증한다.
