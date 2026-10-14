def run(c):
    c.data["material"] = c.upload()
    c.data["foreign_material"] = c.upload(
        "other_teacher", "异班专属资料：量子跃迁验收专属知识，仅供另一个班级。"
    )
    before = c.one("SELECT COUNT(*) AS n FROM materials")["n"]
    c.denied(
        "POST", "/api/materials", "student", 403, files={"file": ("x.txt", b"hello", "text/plain")}
    )
    c.denied(
        "POST", "/api/materials", "teacher", 400, files={"file": ("x.exe", b"hello", "text/plain")}
    )
    c.denied("POST", "/api/materials")
    assert c.one("SELECT COUNT(*) AS n FROM materials")["n"] == before
