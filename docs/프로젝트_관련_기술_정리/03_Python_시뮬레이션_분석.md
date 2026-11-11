# Python 시뮬레이션·분석

## 학습 항목

- 재현 가능한 random seed와 discrete-time simulation
- asyncio producer, schema validation, fixture 생성
- Pandas/Polars 특징 처리와 시간 기반 분할
- scikit-learn baseline, calibration, MLflow 계열 실험 추적
- 임베딩, retrieval, reranking, 근거 평가

## 적용 원칙

시뮬레이터의 장애 정답과 모델 입력을 분리한다. 분석 코드는 온라인 서비스와 학습 파이프라인에서 동일한 전처리 계약을 사용한다. 정확도 하나가 아니라 미탐·오경보·탐지 지연과 판단 보류를 평가한다.

## 실습 증거

동일 seed replay, 시간·기체별 누수 점검, baseline 대 모델 비교, 모델 서버 장애 fallback을 기록한다.
