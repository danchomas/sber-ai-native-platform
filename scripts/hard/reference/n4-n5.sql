SET search_path=raw;
\echo ===== N4 закупки: размер заявки и время согласования
CREATE TEMP TABLE z AS SELECT *, extract(epoch from soglasovana_msk-podana_msk)/3600 h FROM ds_zayavki_na_zakupku_b799b3cf;
SELECT percentile_cont(array[0.25,0.5,0.75,0.9]) within group (order by zaproshennaya_summa_rub) FROM z;
SELECT width_bucket(zaproshennaya_summa_rub, array[10000,50000,100000,500000,1000000]) b, min(zaproshennaya_summa_rub)::int, count(h), round(avg(h)::numeric,1) avg_h, round((percentile_cont(0.5) within group (order by h))::numeric,1) med FROM z GROUP BY 1 ORDER BY 1;
SELECT kategoriya_zakupki, count(h), round(avg(h)::numeric,1) avg_h, round(avg(zaproshennaya_summa_rub)) avg_sum FROM z GROUP BY 1 ORDER BY 3 DESC;
SELECT kategoriya_zakupki, round(avg(h) FILTER (WHERE zaproshennaya_summa_rub>=500000)::numeric,1) big, round(avg(h) FILTER (WHERE zaproshennaya_summa_rub<100000)::numeric,1) small FROM z GROUP BY 1 ORDER BY 1;
\echo ===== N5 поддержка: первый ответ по месяцам
SELECT to_char(date_trunc('month',sozdano_msk),'YYYY-MM') m, count(*), round(100.0*count(pervyy_otvet_msk)/count(*),1) answered, round(avg(extract(epoch from pervyy_otvet_msk-sozdano_msk)/3600)::numeric,2) fr_h FROM ds_tehnicheskaya_podderzhka_04cc29d7 WHERE sozdano_msk>='2025-09-01' GROUP BY 1 ORDER BY 1;
SELECT to_char(date_trunc('week',sozdano_msk),'YYYY-MM-DD') w, count(*), round(100.0*count(pervyy_otvet_msk)/count(*),1) answered, round(avg(extract(epoch from pervyy_otvet_msk-sozdano_msk)/3600)::numeric,2) fr_h FROM ds_tehnicheskaya_podderzhka_04cc29d7 WHERE sozdano_msk>='2026-08-17' GROUP BY 1 ORDER BY 1;
