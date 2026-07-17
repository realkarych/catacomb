**Verdict: ❌ regression**

baseline 5 runs · candidate 5 runs · coverage steps 0.50 phases 0.50

| Verdict | Scope | Key | Name | Metric | Baseline | Candidate | Band | Detail |
|---|---|---|---|---|---|---|---|---|
| regression | total | - | - | cost_usd | 0.10 | 0.20 | [0.07, 0.13] | - |
| ok | total | - | - | duration_ms | 1000.00 | 1000.00 | [700.00, 1300.00] | - |
| ok | total | - | - | error_rate | 0.00 | 0.00 | [0.00, 0.35] | - |
| ok | total | - | - | nodes | 12.00 | 12.00 | [9.00, 15.00] | - |
| ok | total | - | - | tokens_in | 2000.00 | 2000.00 | [1500.00, 2500.00] | - |
| ok | total | - | - | tokens_out | 800.00 | 800.00 | [600.00, 1000.00] | - |
| improvement | phase | pa | alpha | duration_ms | 1000.00 | 600.00 | [700.00, 1300.00] | - |
| notable | phase | pa | alpha | error_rate | 0.00 | 0.60 | [0.00, 0.35] | - |
| insufficient | phase | pb | beta | metrics | 0.00 | 0.00 | - | absent in candidate |
| regression | phase | pb | beta | presence | 1.00 | 0.00 | [0.65, 1.00] | present 5/5 -> 0/5 |
| insufficient | phase | pd | delta | metrics | 0.00 | 0.00 | - | absent in baseline |
| improvement | phase | pd | delta | presence | 0.00 | 1.00 | [0.00, 0.35] | present 0/5 -> 5/5 |
| notable | step | s1 | step-one | duration_ms | 1000.00 | 1600.00 | [700.00, 1300.00] | step alignment coverage 0.50 below floor 0.70 |
| insufficient | step | s2 | step-two | metrics | 0.00 | 0.00 | - | absent in candidate |
| notable | step | s2 | step-two | presence | 1.00 | 0.00 | [0.65, 1.00] | present 5/5 -> 0/5; step alignment coverage 0.50 below floor 0.70 |
