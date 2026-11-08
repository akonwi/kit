// Mica's custom elements, as Solid's JSX sees them.
import "solid-js";

declare module "solid-js" {
  namespace JSX {
    interface IntrinsicElements {
      /** A filterable input and listbox over an input and a datalist (mica/combobox.js). */
      "m-combobox": HTMLAttributes<HTMLElement> & { filter?: "manual" };
    }
  }
}
