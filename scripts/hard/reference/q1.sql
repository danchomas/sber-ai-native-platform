\timing off
SET search_path=raw;
\echo == общие
SELECT min(sozdano_msk), max(sozdano_msk), max(obnovleno_msk), count(*),
  count(nachalo_raboty_msk) has_start, count(zaversheno_msk) has_done FROM ds_zadachi_komand_68b6d258;
\echo == распределение плановых трудозатрат
SELECT planovye_trudozatraty_ch h, count(*) FROM ds_zadachi_komand_68b6d258 GROUP BY 1 ORDER BY 1;
\echo == ожидание начала (часы) по корзинам плана
SELECT width_bucket(planovye_trudozatraty_ch, array[2,4,8,16,24,40]) b, min(planovye_trudozatraty_ch), max(planovye_trudozatraty_ch), count(*),
 round(avg(extract(epoch from nachalo_raboty_msk - sozdano_msk)/3600)::numeric,1) avg_wait_h,
 round((percentile_cont(0.5) within group (order by extract(epoch from nachalo_raboty_msk - sozdano_msk)/3600))::numeric,1) med_wait_h
FROM ds_zadachi_komand_68b6d258 WHERE nachalo_raboty_msk IS NOT NULL GROUP BY 1 ORDER BY 1;
