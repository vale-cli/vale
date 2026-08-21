# A script's leading comment.
Mix.install([:jason])

defmodule Script do
  @moduledoc "One line of documentation."
  @doc since: "1.0.0"
  def run, do: :ok
end
