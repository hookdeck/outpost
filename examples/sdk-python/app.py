import typer

from example.create_destination import run as create_destination
from example.publish_event import run as publish_event
from example.auth import run as auth

app = typer.Typer()

app.command("auth")(auth)
app.command("create-destination")(create_destination)
app.command("publish-event")(publish_event)


if __name__ == "__main__":
    app()
