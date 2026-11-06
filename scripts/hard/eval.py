#!/usr/bin/env python3
"""Прогон сложных вопросов через API поднятого стенда с автооценкой.

Каждый вопрос задаётся в новой сессии несколько раз: одиночный прогон не
показывает, насколько ответ стабилен. Ответ проверяется по ожиданиям из
questions.tsv: вердикт в начале ответа, обязательные упоминания, запрещённые
доводы. Сбои связи с моделью считаются отдельно от ошибок агента, иначе
сетевые обрывы маскируют эффект правок промпта.

    python3 scripts/hard/eval.py --runs 3
    python3 scripts/hard/eval.py --only q1,q2 --runs 5

Сырые ответы и сводка сохраняются в scripts/hard/out/<время>/.
Только стандартная библиотека Python.
"""

import argparse
import concurrent.futures
import datetime
import json
import os
import pathlib
import re
import statistics
import sys
import time
import urllib.request

HERE = pathlib.Path(__file__).resolve().parent

# Предупреждения, по которым видно, что ответ оборвался не по вине агента.
FAILURE = re.compile(
    r"перестала отвечать|недоступна|повторять одни и те же|не уложился", re.IGNORECASE
)


def load_questions(path, only):
    out = []
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip() or line.startswith("#"):
            continue
        cols = (line.split("\t") + [""] * 6)[:6]
        tag, question, verdict, must, forbid, nice = (c.strip() for c in cols)
        if only and tag not in only:
            continue
        out.append(
            {
                "tag": tag,
                "question": question,
                "verdict": verdict,
                "must": [m.strip() for m in must.split("&&") if m.strip()],
                "forbid": forbid,
                "nice": nice,
            }
        )
    return out


def post(api, path, body, timeout):
    req = urllib.request.Request(
        api + path,
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.load(resp)


def ask(api, question, timeout):
    started = time.time()
    try:
        session = post(api, "/sessions", {}, 30)["id"]
        data = post(api, f"/sessions/{session}/messages", {"question": question}, timeout)
        answer = data.get("answer", data)
    except Exception as exc:  # сеть, таймаут, 5xx — это сбой, а не ответ
        answer = {"intent": "ERROR", "text": str(exc)}
    return answer, time.time() - started


def search(pattern, text):
    return bool(pattern) and re.search(pattern, text, re.IGNORECASE) is not None


def grade(q, answer):
    text = (answer.get("text") or "").strip()
    low = text.lower()
    warnings = " ".join(answer.get("warnings") or [])
    failed = answer.get("intent") == "ERROR" or bool(FAILURE.search(warnings))
    verdict_ok = True
    if q["verdict"]:
        head = re.sub(r"^[\W_]+", "", low)[:80]
        verdict_ok = bool(re.match(rf"({q['verdict']})", head)) or bool(
            re.match(rf"({q['verdict']})", (answer.get("verdict") or "").lower())
        )
    missing = [m for m in q["must"] if not search(m, low)]
    forbidden = search(q["forbid"], low)
    return {
        "failed": failed,
        "verdict_ok": verdict_ok,
        "missing": missing,
        "forbidden": forbidden,
        "nice": search(q["nice"], low) if q["nice"] else None,
        "passed": not failed and verdict_ok and not missing and not forbidden,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--api", default=os.environ.get("API", "http://localhost:3000/api/v1"))
    parser.add_argument("--runs", type=int, default=3, help="сколько раз задать каждый вопрос")
    parser.add_argument("--only", default="", help="метки через запятую, например q1,q2")
    parser.add_argument("--parallel", type=int, default=2, help="вопросов одновременно")
    parser.add_argument("--timeout", type=int, default=420, help="предел ответа, секунд")
    parser.add_argument("--questions", default=str(HERE / "questions.tsv"))
    parser.add_argument("--out", default="")
    args = parser.parse_args()

    only = {t.strip() for t in args.only.split(",") if t.strip()}
    questions = load_questions(pathlib.Path(args.questions), only)
    if not questions:
        sys.exit("нет вопросов для прогона")
    out = pathlib.Path(args.out or HERE / "out" / datetime.datetime.now().strftime("%Y%m%d-%H%M%S"))
    out.mkdir(parents=True, exist_ok=True)

    jobs = [(q, run) for run in range(args.runs) for q in questions]
    results = {q["tag"]: [] for q in questions}

    def work(job):
        q, run = job
        answer, secs = ask(args.api, q["question"], args.timeout)
        (out / f"{q['tag']}_{run}.json").write_text(
            json.dumps(answer, ensure_ascii=False, indent=1), encoding="utf-8"
        )
        return q, run, answer, secs, grade(q, answer)

    with concurrent.futures.ThreadPoolExecutor(max_workers=max(args.parallel, 1)) as pool:
        for q, run, answer, secs, g in pool.map(work, jobs):
            results[q["tag"]].append((answer, secs, g))
            mark = "СБОЙ" if g["failed"] else ("ок" if g["passed"] else "НЕТ")
            why = []
            if not g["verdict_ok"]:
                why.append("вердикт")
            if g["missing"]:
                why.append("нет: " + ", ".join(g["missing"]))
            if g["forbidden"]:
                why.append("запрещённый довод")
            text = (answer.get("text") or "").replace("\n", " ")[:110]
            print(f"{q['tag']:>4} #{run} {mark:<4} {secs:5.0f}с {'; '.join(why):<30} {text}", flush=True)

    lines = [
        "| Вопрос | Зачёт | Сбои | Желательно | Время, с (медиана) | Шагов (медиана) |",
        "|---|---|---|---|---|---|",
    ]
    total = passed = failures = 0
    for q in questions:
        rows = results[q["tag"]]
        ok = sum(g["passed"] for _, _, g in rows)
        fails = sum(g["failed"] for _, _, g in rows)
        nice = [g["nice"] for _, _, g in rows if g["nice"] is not None]
        secs = statistics.median(s for _, s, _ in rows)
        steps = statistics.median(len(a.get("steps") or []) for a, _, _ in rows)
        total += len(rows)
        passed += ok
        failures += fails
        lines.append(
            f"| {q['tag']} | {ok}/{len(rows)} | {fails} | "
            f"{f'{sum(nice)}/{len(nice)}' if nice else '—'} | {secs:.0f} | {steps:.0f} |"
        )
    graded = total - failures
    lines.append("")
    lines.append(
        f"Итого: зачёт {passed}/{total} ({100 * passed / total:.0f}%), "
        f"без сбоев связи {passed}/{graded} ({100 * passed / max(graded, 1):.0f}%), сбоев {failures}."
    )
    summary = "\n".join(lines)
    (out / "summary.md").write_text(summary + "\n", encoding="utf-8")
    print()
    print(summary)
    print(f"\nОтветы: {out}")


if __name__ == "__main__":
    main()
