import { useNavigate } from "react-router-dom";
import { Loading } from "../../../common/Icons";
import { useCreateDestinationContext } from "../CreateDestination";

export default function TypeStep() {
  const { stepValues, setStepValues, destinationTypes, hasDestinationTypes, nextPath } =
    useCreateDestinationContext();
  const navigate = useNavigate();

  return (
    <>
      <div className="create-destination__step__header">
        <h1 className="title-xl">Select destination type</h1>
        <p className="body-m muted">
          Select the destination type you want to send to your destination
        </p>
      </div>
      <form
        onChange={(e) => {
          const formData = new FormData(e.currentTarget);
          const values = Object.fromEntries(formData.entries());
          if (values.type) {
            setStepValues((prev) => ({ ...prev, ...values }));
            if (nextPath) {
              navigate(nextPath);
            }
          }
        }}
        onSubmit={(e) => e.preventDefault()}
      >
        <div className="create-destination__step__fields">
          {hasDestinationTypes ? (
            <div className="destination-types">
              <div className="destination-types__container">
                {Object.values(destinationTypes).map((destination) => (
                  <label
                    key={destination.type}
                    className="destination-type-option"
                  >
                    <input
                      type="radio"
                      name="type"
                      value={destination.type}
                      required
                      className="destination-type-radio"
                      defaultChecked={stepValues.type === destination.type}
                    />
                    <div className="destination-type-content">
                      <h3 className="subtitle-l">
                        <span
                          className="destination-type-content__icon"
                          dangerouslySetInnerHTML={{
                            __html: destination.icon,
                          }}
                        />{" "}
                        {destination.label}
                      </h3>
                      <p className="body-m muted">{destination.description}</p>
                    </div>
                  </label>
                ))}
              </div>
            </div>
          ) : (
            <div>
              <Loading />
            </div>
          )}
        </div>
      </form>
    </>
  );
}
