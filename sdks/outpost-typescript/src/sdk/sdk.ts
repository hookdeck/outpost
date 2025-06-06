/*
 * Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.
 */

import { ClientSDK } from "../lib/sdks.js";
import { Destinations } from "./destinations.js";
import { Events } from "./events.js";
import { Health } from "./health.js";
import { Publish } from "./publish.js";
import { Schemas } from "./schemas.js";
import { Tenants } from "./tenants.js";
import { Topics } from "./topics.js";

export class Outpost extends ClientSDK {
  private _health?: Health;
  get health(): Health {
    return (this._health ??= new Health(this._options));
  }

  private _tenants?: Tenants;
  get tenants(): Tenants {
    return (this._tenants ??= new Tenants(this._options));
  }

  private _destinations?: Destinations;
  get destinations(): Destinations {
    return (this._destinations ??= new Destinations(this._options));
  }

  private _publish?: Publish;
  get publish(): Publish {
    return (this._publish ??= new Publish(this._options));
  }

  private _schemas?: Schemas;
  get schemas(): Schemas {
    return (this._schemas ??= new Schemas(this._options));
  }

  private _topics?: Topics;
  get topics(): Topics {
    return (this._topics ??= new Topics(this._options));
  }

  private _events?: Events;
  get events(): Events {
    return (this._events ??= new Events(this._options));
  }
}
