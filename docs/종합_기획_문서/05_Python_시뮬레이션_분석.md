# Python 시뮬레이션·분석 아키텍처

## 1. 프로세스 분리

```text
python/
├─ patrol-simulator/
│  ├─ flight_model
│  ├─ battery_model
│  ├─ environment_model
│  ├─ sensor_model
│  └─ fault_injector
└─ analysis-service/
   ├─ feature_pipeline
   ├─ anomaly_detection
   ├─ rag
   ├─ llm
   └─ evaluation
```

시뮬레이터와 분석 서비스는 같은 Python을 사용하더라도 별도 프로세스·이미지·권한으로 운영한다.

## 2. 순찰 Simulator

- 도시·공장 구역과 순찰 Route 생성
- Checkpoint 통과·Coverage 계산용 위치 생성
- 배터리·통신 품질·탑재체 상태 모델
- 손실·지연·역순·중복·단절·ACK 손실 주입
- 고정 Seed와 Scenario Manifest
- 실제 주입 원인과 관측 Event 분리

## 3. 분석 서비스

- 경로 이탈·속도 불연속·통신 열화 Feature
- 규칙·EWMA·Isolation Forest 비교
- Checkpoint 누락과 Coverage 이상 후보
- Incident Timeline과 절차서 Hybrid RAG
- 로컬 LLM의 근거 기반 설명·보고서 초안

## 4. 안전 경계

- 분석 서비스에는 Command Credential을 주지 않는다.
- 입력 품질 미달 시 `INSUFFICIENT_EVIDENCE`를 반환한다.
- 얼굴 인식·개인 식별·개인 행동 추론은 구현하지 않는다.
- AI 중단 시 규칙 기반 경보와 순찰 관제는 유지한다.
