# React·TypeScript·GIS 관제 화면

## 학습 항목

- strict TypeScript와 schema-generated type
- TanStack Query 계열 서버 상태, UI 로컬 상태 분리
- WebSocket reconnect, snapshot+delta, sequence gap 처리
- MapLibre/지도 레이어, 좌표계, 대량 feature 렌더링
- 접근성, 위험 색상 외 중복 표현, 가상화

## 적용 원칙

클라이언트가 명령 성공을 추정하지 않는다. 서버의 명령 상태와 관측 증거를 표시한다. 마지막 위치는 관측 시각과 신선도를 함께 보여주며 오래된 값을 현재 위치처럼 이동시키지 않는다.

## 실습 증거

네트워크 단절·재연결, 역순 delta, 100대 가상 기체, 키보드 조작과 색각 보조 표현을 시험한다.
