import { Spinner } from "../Spinner";
import { theme } from "../theme";

export function InlineSpinner() {
	return <Spinner fg={theme.toolText} />;
}
