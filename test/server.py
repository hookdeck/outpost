import http.server
import logging
import sys


class SimpleHTTPRequestHandler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        logging.basicConfig(stream=sys.stdout, level=logging.INFO)
        post_data = self.rfile.read()
        logging.info(
            "POST request,\nPath: %s\nHeaders:\n%s\n\nBody:\n%s\n",
            str(self.path),
            str(self.headers),
            post_data.decode("utf-8"),
        )
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"POST request received")

    def do_GET(self):
        logging.basicConfig(stream=sys.stdout, level=logging.INFO)
        logging.info(
            "GET request,\nPath: %s\nHeaders:\n%s\n",
            str(self.path),
            str(self.headers),
        )
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"GET request received")


if len(sys.argv) != 2:
    print("Usage: python server.py <port>")
    sys.exit(1)

port = int(sys.argv[1])
httpd = http.server.HTTPServer(("127.0.0.1", port), SimpleHTTPRequestHandler)
httpd.serve_forever()
