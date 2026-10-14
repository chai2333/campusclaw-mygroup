from openapi_spec_validator import validate


def run(c):
    doc = c.json("GET", "/openapi.json")
    validate(doc)
    assert doc["components"]["securitySchemes"]["bearerAuth"]["scheme"] == "bearer"
    assert doc["paths"]["/api/login"]["post"]["security"] == []
    assert (
        "text/event-stream"
        in doc["paths"]["/api/conversations/{id}/messages"]["post"]["responses"]["200"]["content"]
    )
    c.data["openapi"] = doc
    c.contract = doc
