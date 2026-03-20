<?xml version="1.0" encoding="UTF-8"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL">
  <Header id="MinV17" version="1" revision="0"/>
  <IED name="IED1">
    <AccessPoint name="AP1">
      <Server>
        <LDevice inst="LD1">
          <LN0 lnClass="LLN0" inst="" lnType="LLN0_Type"/>
        </LDevice>
      </Server>
    </AccessPoint>
  </IED>
  <DataTypeTemplates>
    <LNodeType id="LLN0_Type" lnClass="LLN0">
      <DO name="Mod" type="INC"/>
    </LNodeType>
    <DOType id="INC" cdc="INC">
      <DA name="stVal" fc="ST" bType="INT32"/>
    </DOType>
  </DataTypeTemplates>
</SCL>
