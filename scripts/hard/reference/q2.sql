SET search_path=raw;
CREATE TEMP TABLE t AS SELECT *, extract(epoch from zaversheno_msk - nachalo_raboty_msk)/3600 dur,
  extract(epoch from zaversheno_msk - sozdano_msk)/3600 lead
  FROM ds_zadachi_komand_68b6d258;
\echo == по неделям создания (с июля 2026): наивная длительность, доля завершённых, завершённых за 3 суток от создания
SELECT date_trunc('week',sozdano_msk)::date wk, count(*) n,
 round(avg(dur)::numeric,1) avg_dur_h, round(avg(lead)::numeric,1) avg_lead_h,
 round(100.0*count(zaversheno_msk)/count(*),1) pct_done,
 round(100.0*count(*) filter (where lead<=72)/count(*),1) pct_done_72h,
 round(avg(fakticheskie_trudozatraty_ch)::numeric,2) fact_h, round(avg(planovye_trudozatraty_ch)::numeric,2) plan_h
FROM t WHERE sozdano_msk >= '2026-06-29' GROUP BY 1 ORDER BY 1;
\echo == статусы по неделям создания (последние 5 недель)
SELECT date_trunc('week',sozdano_msk)::date wk, status, count(*) FROM t WHERE sozdano_msk >= '2026-08-31' GROUP BY 1,2 ORDER BY 1,2;
\echo == по неделям ЗАВЕРШЕНИЯ
SELECT date_trunc('week',zaversheno_msk)::date wk, count(*), round(avg(dur)::numeric,1) dur, round(avg(lead)::numeric,1) lead FROM t WHERE zaversheno_msk >= '2026-07-27' GROUP BY 1 ORDER BY 1;
