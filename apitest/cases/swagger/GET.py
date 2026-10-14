def run(c):
    response = c.request("GET", "/swagger/")
    assert (
        response.status_code == 200
        and "SwaggerUIBundle" in response.text
        and "/openapi.json" in response.text
    )
    for name in ("swagger-ui-bundle.js", "swagger-ui.css"):
        r = c.request("GET", "/swagger/assets/" + name)
        assert r.status_code == 200 and len(r.content) > 1000
    assert c.request("GET", "/swagger/unknown").status_code == 404
