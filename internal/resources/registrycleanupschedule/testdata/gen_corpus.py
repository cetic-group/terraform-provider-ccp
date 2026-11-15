# Régénère api_corpus.json en jugeant chaque cas avec le code RÉEL de l'API.
#
# Usage (depuis un dossier vide, pydantic 2 installé) :
#   P=<chemin de cetic-cloud-platform>; mkdir -p app/schemas app/services
#   git -C $P show origin/main:apps/api/app/schemas/registry_retention.py > app/schemas/registry_retention.py
#   git -C $P show origin/main:apps/api/app/services/registry_cleanup.py > app/services/registry_cleanup.py
#   touch app/__init__.py app/schemas/__init__.py app/services/__init__.py
#   printf 'ACTION_EXPIRE="expire"\nACTION_KEEP="keep"\nclass Regle:\n    def __init__(self, **kw): pass\n' > app/services/registry_retention.py
#   python3 <ce fichier> > api_corpus.json
#
# `registry_retention` est remplacé par un bouchon : la validation n'en lit que
# des constantes. Graine fixe : le corpus est reproductible.
import json, random, sys
sys.path.insert(0, '.')
from pydantic import ValidationError
from app.schemas.registry_retention import CleanupScheduleCreate as C
from app.schemas.registry_retention import CleanupScheduleUpdate as U, _valider_critere

random.seed(1984)
BASE = dict(name="nightly", repo_patterns=["app/*"], tag_patterns=["*-rc-*"], critere="all", valeur=None, cron=None)
cases = []
def add(**over):
    c = dict(BASE); c.update(over); cases.append(c)

for n in ["", " ", "a", "a"*64, "a"*65, " "+"a"*63, " "+"a"*64, "-a", "_a", "a-b c.d_e", "é", "aé",
          "a\tb", "a\n", "\x1ca", "a\x1f", "Nightly 1", "a/b", " a ", "a ", "a  b", "1", "a:b"]:
    add(name=n)
repos = ["*", "?*", "*?", "??*", "*/*", "?*/*", "*/*/*", "/*", "*/", "app/*", "APP/*", "a**b", "", " ", " app/* ",
         "a"*255, "a"*256, " "+"a"*255, "app/[x]", "app\\x", "é/*", "/", "//*", "?", "app", "team/*", "*-dev",
         "a.b_c-d/e?", "app/*\n", "\x1capp"]
tags = ["*", "?*", "*?", "*-rc-*", "A_b.c-1", "a"*128, "a"*129, "a/b", "**", " * ", "v1+x", "", "é*", "?", "*\x1f"]
for r in repos:
    add(repo_patterns=[r])
for t in tags:
    add(tag_patterns=[t])
for lst in [[], ["a"]*1, [f"a{i}" for i in range(10)], [f"a{i}" for i in range(11)], ["a", "a"], [" a", "a"], ["a", "b"]]:
    add(repo_patterns=lst); add(tag_patterns=lst)
crons = [None, "", "  ", "\t", "0 3 * * *", "0  3 * * *", " 0 3 * * * ", "*/5 * * * *", "*/0 * * * *", "*/00 * * * *",
         "*/99 * * * *", "*/100 * * * *", "60 * * * *", "59 23 31 12 *", "0 24 * * *", "0 0 0 * *", "0 0 1 * 1",
         "0 0 * * 7", "0 0 * * 0-6", "0 0 * * 6-0", "0 0 * * 3-3", "1,2 * * * *", "*,5 * * * *", "5,* * * * *",
         "1-5/2 * * * *", "0 3 * *", "0 3 * * * *", "0\t3 * * *", "0\x1c3 * * *", "0 3 * * *\x1f", "0"+" "*60+"3 * * *",
         "0 3 * * *" + " "*56, "00 03 * * *", "000 3 * * *", "0 3 */2 * *", "0 3 */2 * 1", "0 3 1-31 1-12 *",
         ",1 * * * *", "1, * * * *", "1,,2 * * * *", "a * * * *", "0 3 * JAN *", "-1 * * * *", "1- * * * *",
         "0 3 ? * *", "0 3 L * *", "@daily", "0 3 * * *é", "0 3 * * ٣", "0 0 1 1 0", "0 0 * 0 *", "0 0 * 13 *",
         "*/5 */2 * * 1-5", "0 0 * * 1,3,5", "0 0 1,15 * *"]
for c in crons:
    add(cron=c)
uni = (["*"], ["*"]); restr = (["app/*"], ["*-dev"]); unir = (["*/*"], ["?*"]); half = (["*", "app"], ["v1", "*"])
for (rp, tp) in [uni, restr, unir, half, (["*"], ["v*"]), (["app/*", "*"], ["*"]), (["é"], ["*"])]:
    for crit, vals in [("all", [None, 0, 1]), ("older_than_days", [None, 0, 1, 6, 7, 3650, 3651]),
                       ("keep_last_n", [None, 0, 1, 1000, 1001])]:
        for v in vals:
            add(repo_patterns=rp, tag_patterns=tp, critere=crit, valeur=v)

alpha_cron = list("0123456789*/,-") + [" ", "  ", "\t"]
alpha_pat = list("abzAZ09._-/*? ") + ["é"]
for _ in range(400):
    f = []
    for i in range(random.choice([4, 5, 5, 5, 6])):
        f.append("".join(random.choice(alpha_cron[:-3]) for _ in range(random.randint(1, 5))))
    add(cron=" ".join(f))
for _ in range(300):
    add(repo_patterns=["".join(random.choice(alpha_pat) for _ in range(random.randint(0, 6)))],
        tag_patterns=["".join(random.choice(alpha_pat) for _ in range(random.randint(0, 6)))],
        critere=random.choice(["all", "older_than_days", "keep_last_n"]),
        valeur=random.choice([None, 1, 3, 7, 30]))

out = []
for c in cases:
    body = {k: v for k, v in c.items()}
    try:
        C(**body); ok = True
    except ValidationError:
        ok = False
    # PATCH tel que l'envoie le provider : les 7 champs, toujours. Reproduit
    # update_cleanup_schedule (registries.py) : schéma CleanupScheduleUpdate,
    # refus des null sur les champs non nullables, puis _valider_critere sur
    # les valeurs fusionnées — ici toutes celles du corps.
    try:
        u = U(**{**body, "enabled": True})
        champs = u.model_fields_set
        if any(n in champs and getattr(u, n) is None
               for n in ("name", "repo_patterns", "tag_patterns", "critere", "enabled")):
            raise ValueError("nul")
        _valider_critere(u.critere, u.valeur, list(u.repo_patterns), list(u.tag_patterns))
        ok_patch = True
    except (ValidationError, ValueError):
        ok_patch = False
    out.append({**c, "api_accepts": ok, "api_accepts_patch": ok_patch})
json.dump(out, sys.stdout, ensure_ascii=False, indent=0)
print(file=sys.stderr, end=f"{len(out)} cas, {sum(o['api_accepts'] for o in out)} acceptés (POST), "
      f"{sum(o['api_accepts_patch'] for o in out)} (PATCH), "
      f"{sum(o['api_accepts'] != o['api_accepts_patch'] for o in out)} divergents\n")
