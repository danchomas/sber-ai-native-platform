SET search_path=raw;
\echo ===== H3 доставка: опоздания по перевозчику и способу
SELECT status, count(*) FROM ds_dostavka_4f67ae5a GROUP BY 1 ORDER BY 2 DESC;
SELECT perevozchik, count(*) FILTER (WHERE dostavleno_msk IS NOT NULL) delivered, round(100.0*avg((dostavleno_msk>obeschannyy_srok_msk)::int),1) late_pct FROM ds_dostavka_4f67ae5a WHERE dostavleno_msk IS NOT NULL GROUP BY 1 ORDER BY 3 DESC;
SELECT perevozchik, sposob_dostavki, count(*) n, round(100.0*avg((dostavleno_msk>obeschannyy_srok_msk)::int),1) late_pct FROM ds_dostavka_4f67ae5a WHERE dostavleno_msk IS NOT NULL GROUP BY 1,2 ORDER BY 1,4 DESC;
\echo ===== H4 доставка: вес и возвраты
SELECT min(ves_kg), max(ves_kg), round(avg(ves_kg),1), percentile_cont(array[0.5,0.75,0.9]) within group (order by ves_kg) FROM ds_dostavka_4f67ae5a;
SELECT width_bucket(ves_kg, array[1,2,5,10,20,30]) b, min(ves_kg), max(ves_kg), count(*), round(100.0*avg((vozvrascheno_otpravitelyu_msk IS NOT NULL)::int),2) ret_pct, round(100.0*avg((status like 'Возвр%')::int),2) ret_status FROM ds_dostavka_4f67ae5a GROUP BY 1 ORDER BY 1;
SELECT sposob_dostavki, round(100.0*avg((vozvrascheno_otpravitelyu_msk IS NOT NULL)::int) FILTER (WHERE ves_kg>=10),2) heavy, round(100.0*avg((vozvrascheno_otpravitelyu_msk IS NOT NULL)::int) FILTER (WHERE ves_kg<10),2) light, round(100.0*avg((ves_kg>=10)::int),1) heavy_share FROM ds_dostavka_4f67ae5a GROUP BY 1;
\echo ===== H5 задачи: недооценка трудозатрат по типу
SELECT tip_zadachi, count(*) n, round(avg(fakticheskie_trudozatraty_ch/planovye_trudozatraty_ch)::numeric,2) avg_ratio, round(sum(fakticheskie_trudozatraty_ch)/sum(planovye_trudozatraty_ch)::numeric,2) sum_ratio, round(100.0*avg((fakticheskie_trudozatraty_ch>planovye_trudozatraty_ch)::int),1) over_pct FROM ds_zadachi_komand_68b6d258 WHERE status='Завершена' GROUP BY 1 ORDER BY 4 DESC;
\echo ===== H6 задачи: просрочка по кварталам и направлениям
SELECT to_char(date_trunc('quarter',sozdano_msk),'YYYY-"Q"Q') q, round(100.0*avg((zaversheno_msk>srok_vypolneniya_msk)::int) FILTER (WHERE zaversheno_msk IS NOT NULL),1) late_pct, count(*) FROM ds_zadachi_komand_68b6d258 GROUP BY 1 ORDER BY 1;
SELECT napravlenie, to_char(date_trunc('quarter',sozdano_msk),'YYYY-"Q"Q') q, round(100.0*avg((zaversheno_msk>srok_vypolneniya_msk)::int) FILTER (WHERE zaversheno_msk IS NOT NULL),1) late_pct FROM ds_zadachi_komand_68b6d258 WHERE sozdano_msk >= '2025-10-01' GROUP BY 1,2 ORDER BY 1,2;
