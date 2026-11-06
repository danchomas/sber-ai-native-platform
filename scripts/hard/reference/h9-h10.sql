SET search_path=raw;
\echo ===== H9 реклама: ROMI по площадкам и аудиториям
SELECT ploschadka, round(sum(rashody_rub)) spend, round(sum(vyruchka_po_reklame_rub)) rev, round((100*(sum(vyruchka_po_reklame_rub)-sum(rashody_rub))/sum(rashody_rub))::numeric,1) romi FROM ds_reklamnye_kampanii_daedfb94 GROUP BY 1 ORDER BY 4 DESC;
SELECT auditoriya, round((100*(sum(vyruchka_po_reklame_rub)-sum(rashody_rub))/sum(rashody_rub))::numeric,1) romi, round((100*sum(rashody_rub)/sum(sum(rashody_rub)) over())::numeric,1) spend_share FROM ds_reklamnye_kampanii_daedfb94 GROUP BY 1 ORDER BY 2 DESC;
SELECT ploschadka, auditoriya, round((100*(sum(vyruchka_po_reklame_rub)-sum(rashody_rub))/sum(rashody_rub))::numeric,1) romi, round((100.0*sum(rashody_rub)/sum(sum(rashody_rub)) over (partition by ploschadka))::numeric,1) mix FROM ds_reklamnye_kampanii_daedfb94 GROUP BY 1,2 ORDER BY 1,3 DESC;
\echo ===== H10 продажи: скидка и количество, по категориям
SELECT width_bucket(skidka, array[1,10,20,30,40]) b, min(skidka), max(skidka), count(*), round(avg(kolichestvo_sht),3) qty FROM ds_c_01_prodazhi_5e1ce493 WHERE tip_operacii='Продажа' GROUP BY 1 ORDER BY 1;
SELECT kategoriya_tovara, round(avg(skidka),1) avg_disc, round(avg(kolichestvo_sht),2) qty, round(corr(skidka, kolichestvo_sht)::numeric,3) r FROM ds_c_01_prodazhi_5e1ce493 WHERE tip_operacii='Продажа' GROUP BY 1 ORDER BY 2 DESC;
SELECT round(corr(skidka, kolichestvo_sht)::numeric,3) r_all FROM ds_c_01_prodazhi_5e1ce493 WHERE tip_operacii='Продажа';
